# durable

A from-scratch durable execution engine for LLM agents. Single-machine, single-process, Go.

> Built to understand durable execution from the inside — not to compete with Temporal, Restate, Inngest, or any of the production systems that already solve this well. If you need durable execution in production, use one of those. If you want to understand *how they work*, this might help.

## The problem, in one line

**Crash-resistant, replayable, auditable LLM agent runs.**

If the process gets killed mid-tool-call and restarted, the agent should resume from exactly where it left off — without losing progress and without re-executing side effects against the outside world.

## Why this matters

Agents do work in the real world. They send emails, charge cards, write rows to databases, post messages to Slack. Each of those is a *side effect* — once it has happened, you can't pretend it hasn't. An agent crashing halfway through a multi-step task creates two failure modes that are both unacceptable:

1. **Lost progress.** The model's reasoning, the partial results, the accumulated context — all gone. The agent restarts from scratch and the user notices.
2. **Double effects.** The retry re-runs steps whose side effects already happened. Two emails. Two charges. Two rows.

Production agent platforms (LangGraph, the OpenAI/Vercel agent SDKs, Restate, Inngest, Temporal, DBOS, Hatchet, …) solve this with **durable execution**: write the agent's intent to disk *before* the side effect, and use the journaled record to deduplicate retries on recovery. This project rebuilds that primitive from scratch — write-ahead log, deterministic replay, idempotency keys, the whole stack — for one purpose: to understand what those production systems are actually doing under the surface.

It is an internals exercise. It is not a product.

## The load-bearing idea: write-ahead ordering

The whole system rests on one invariant:

> The intent to perform a side effect is written and `fsync`-ed to disk *before* the side effect is invoked.

If you flip those two — invoke first, log after — a crash in between leaves you with a side effect that happened in the world with no record of it, and the retry will execute it a second time. That is the canonical bug durable execution exists to prevent. The full sequence is walked through in [`docs/architecture.md`](./docs/architecture.md).

## System architecture

```mermaid
flowchart LR
    subgraph Binary["durable (single process, single machine)"]
        Agent["Agent loop<br/>model call → tool call → repeat"]
        WAL["WAL Manager<br/>append + replay"]
        Tools["Tool registry<br/>idempotency keys + adapters"]
        Fold["Fold<br/>log → in-memory state"]
    end

    Disk[("Log file<br/>on disk")]
    LLM["LLM API"]
    Ext["External systems<br/>HTTP, DB, ..."]

    Agent -->|"1. intent"| WAL
    WAL <-->|"fsync"| Disk
    Agent -->|"2. invoke"| Tools
    Tools -->|"side effect"| Ext
    Agent -->|"3. result"| WAL
    Agent <-->|"model call"| LLM
    Disk -.->|"on startup, replay"| Fold
    Fold -.->|"rebuilds state"| Agent
```

The numbered arrows are the canonical step the system performs over and over:

1. **Intent.** The agent journals what it's *about* to do (with a stable idempotency key) before any external action happens.
2. **Invoke.** The tool runs. The side effect may or may not succeed — that's fine.
3. **Result.** Whatever happened gets journaled.

If the process crashes between 1 and 3, **replay** sees an intent without a matching result, re-invokes the tool, and the idempotency key keeps the external system from acting twice. The LLM and external systems sit *outside* the durability boundary — the WAL only journals what crosses it.

## The WAL

The WAL package is the foundation everything else sits on. Today it implements **L1**: framing, append, sequential read. Layers L2 (checksums), L3 (crash recovery), L4 (group commit) are next.

```mermaid
flowchart TB
    Caller["Caller<br/>(agent loop, demo binary, ...)"]

    subgraph Pkg["wal package"]
        Manager["Manager<br/>NewManager · Append · Replay · Close"]
        Writer["Writer<br/>O_APPEND · O_WRONLY · O_CREATE"]
        Reader["Reader<br/>bufio + Next() returning (Record, error)"]
    end

    Disk[("Log file<br/>append-only")]

    Caller -->|"Append(rec)"| Manager
    Caller -->|"Replay(fn)"| Manager
    Manager -->|"owns for life of run"| Writer
    Manager -.->|"opens fresh per Replay"| Reader
    Writer -->|"Encode then Write"| Disk
    Reader -->|"Read then DecodeRecord"| Disk
```

The Writer owns one file descriptor for the entire run. The Reader is created fresh for each `Replay` call, drained, and discarded — writes are continuous (every step), reads are bursty (startup + debug). Different lifecycles, different ownership models.

**On-disk record format:**

```
+------------------+----------------+---------------------------+
| length (4 bytes) | type (1 byte)  | payload (length-1 bytes)  |
+------------------+----------------+---------------------------+
```

- `length` — `uint32` little-endian. Total size of `(type + payload)`, not including itself.
- `type` — 1-byte tag. Today only `RecordTypeRaw = 0x01`. L6 adds `RunStart`, `ModelCallIntent`, `ToolCallResult`, etc.
- `payload` — arbitrary bytes.

There is no separator between records. The length field is what tells the reader where one ends and the next begins.

## Try it

The demo binary takes a string, splits it on whitespace, writes each word as a `Record`, and then replays the log:

```sh
go build -o durable .
./durable "hi my name is prashant"
```

```
appended 5 records to durable.log

replay:
  [0] type=0x01 payload="hi"
  [1] type=0x01 payload="my"
  [2] type=0x01 payload="name"
  [3] type=0x01 payload="is"
  [4] type=0x01 payload="prashant"
```

Run it again and the new words are appended onto the existing log — the file is never truncated:

```sh
./durable "and now i appended more"
```

```
  [0] type=0x01 payload="hi"
  ...
  [5] type=0x01 payload="and"
  [6] type=0x01 payload="now"
  [7] type=0x01 payload="i"
  [8] type=0x01 payload="appended"
  [9] type=0x01 payload="more"
```

Peek at the framing on disk:

```sh
hexdump -C durable.log
```

```
00000000  03 00 00 00 01 68 69 03  00 00 00 01 6d 79 05 00  |.....hi.....my..|
00000010  00 00 01 6e 61 6d 65 03  00 00 00 01 69 73 09 00  |...name.....is..|
...
```

`03 00 00 00` = length 3 (little-endian). `01` = `RecordTypeRaw`. `68 69` = `"hi"`. Each record is exactly `5 + len(payload)` bytes on disk.

Run the test suite:

```sh
go test ./wal/... -race
```
