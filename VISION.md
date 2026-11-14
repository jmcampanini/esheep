# Vision

`esheep` exists so that the sessions a person has had with coding agents stay findable and readable long after the work ended.
It serves people and agents who need evidence from past sessions, and it turns the transcripts that Claude Code, Pi, Codex, ChatGPT Work, and Claude Cowork leave on disk into listed sessions, addressed search hits, and complete events.
It owns exactly one thing: the read path from harness-owned transcripts to that evidence.


## Transcripts stay where the harness put them

esheep reads transcripts in place and never modifies them.
It writes nothing: no index, no cache, no derived copy, no state between runs.
It never contacts another machine and needs no external program.
A continuation cursor encodes its own position, so paging keeps no state either.
A feature that needs a write path or a network path belongs in a different tool.
Remote search over ssh and memory recording were both shipped and then removed for this reason.
When a scan grows too slow, the answer is a narrower filter before the scan, not a store that esheep keeps.
Moving sessions into a database that esheep owns would make esheep a different kind of tool, and that change happens only as a deliberate decision about what esheep is, never as a side effect of a performance fix.


## esheep is for past sessions

esheep finds sessions that already happened and reads them.
It is a command-line program: every command reads what is on disk, prints, and exits.
It does not follow or watch a session in progress.
Skill and agents-file management was nearly half the code, and it was removed whole.
Work that a pipe can do stays outside: counts, sums, and rankings belong to `jq` and the caller.
Helping a caller act on a found session, for example by printing bare session IDs, is not off mission, but it needs a specific case that a pipe through `jq` does not already serve.
Finding evidence and inspecting it are separate steps: search locates events, and peek returns them in full.


## Every result stays true to what is recorded

esheep searches what is recorded and stays as true to the transcript's content as it can.
What the harness did not record stays unknown: esheep does not infer attribution, origin, or failure status.
Deriving a fact that the transcript does not state outright is a rare exception that needs its own justification.
The one accepted departure is secret masking: esheep may replace text that matches a specific, simple pattern, and masking that needs heuristics or broad detection is out.
Masked text is marked as masked.
A report never claims more than esheep read.
A missing root, an unreadable transcript, and an event without a usable timestamp are each diagnosed, and a failed read makes the report incomplete.
A limited response reports what matched, what it returned, and what it omitted as separate facts.
Time filters select recorded event time, because file modification time misstates when activity happened.
Every hit names its canonical source and its position there, which for a transcript file is the path and line, so a reader can open the source and check it.


## The harness formats are esheep's problem

Each harness stores different records, and those formats change without notice.
esheep decodes them so that no consumer parses a native transcript.
esheep supports the harnesses its author uses or has a reasonable chance of using, and an adapter for any other harness is declined however complete it is.
How a supported harness stores its sessions does not decide scope: plain JSONL, compressed JSONL, and a SQLite database are all read in place.
A new on-disk form of a supported harness is in scope as soon as sessions go missing because of it.
Each harness has one session location, which configuration, an environment variable, or a flag can point elsewhere.
To read a second location, such as a folder synced from another machine, run esheep again against it and merge the results outside esheep.
esheep does not label or tell apart several instances of one harness.
Known decoding limits are stated in command help, and `--raw` matches the original records.


## Output is sized for the reader

Agents and scripts read esheep's output, and an agent's tool keeps only the first part of a long reply.
Search and peek bound every response by default, keep it valid JSON, and name how to get the rest.
Defaults come from measurements on real history.
Payload goes to stdout and diagnostics go to stderr in every output mode.
Commands accept only the arguments shown and never prompt.
Every setting has a default, so esheep works without a configuration file.


## Contracts live in the binary

Command help is the canonical reference for every flag, default, exit status, and JSON field.
The README is a landing page.
A breaking change is a clean break: no alias, no migration error, no compatibility guard.
Removing a feature removes its commands, configuration keys, and documentation in the same change.
The JSON output carries no stability promise yet, so a change to it is a clean break too.
That promise waits for a strong, concrete benefit, most likely at the scale where a schema would map the output onto a database structure.


## Scope

esheep is not a skill manager, not a memory store, not a backup or sync tool, not a session browser, not a converter to a common transcript format, and not a database.
It is not a daemon, not a service, and not an MCP server.
It ships GitHub release archives for macOS and Linux and maintains no second publishing channel.
Each new dependency arrives alone, with its reason stated.

A change aligns when it helps a person or an agent find or read a recorded session from a harness the author uses, reads only local transcripts, leaves nothing on disk, stays true to what is recorded, and reports its own coverage and limits.
A change should be resisted when it adds a write path or a network path, keeps state between runs or a process running, infers something the transcript does not record, supports a harness the author has no reasonable chance of using, tells apart several instances of one harness, returns unbounded output by default, or does a job that a pipe to another tool already does.
