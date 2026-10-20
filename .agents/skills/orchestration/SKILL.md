---
name: orchestration
description: Splitting work into bounded subagent workstreams and keeping verification and the record in the orchestrator. Use this when a task has more than one step, when several things could be done at once, when a diagnosis or fix spans machines, or on any work in the homelab estate or this dotfiles repo. Covers how to brief a subagent, what never delegates (verification, the commit, the docs), which splits collide, and the cheap commands that prove a report is true.
---

# Orchestration

The main session is an orchestrator, not a worker. Its value is the thread: it knows what was
asked, what has been measured, what is still open, and what the operator was last told. Every
minute it spends running commands is a minute that thread is not being maintained, and the
context it fills with tool output is context it will need for the decision at the end.

This skill is the operational detail behind rule 23 of `~/AGENTS.md` and behind the estate's
own contract (see [Where it is also written down](#where-it-is-also-written-down)).

## When this applies

- **Any task with more than one step.** Two commands that inform each other are still a split:
  one subagent measures, the orchestrator decides.
- **Every task in the homelab estate** (VM 108, the Proxmox host, any stack in
  `docker-compose-services/`).
- **Every task in this dotfiles repo** (`/home/manu343726` is the repo root and the real
  `$HOME`).

**The one exception:** a single read-only question that one command answers. "What kernel is
running", "which commits are on origin", "is the tunnel up". Answer it and move on. Wrapping a
one-command question in a subagent costs more than the answer is worth and teaches nothing.

Two commands where the second depends on the first is not that exception. That is a workstream.

## The shape

**One bounded workstream per subagent.** A workstream is a unit with a single deliverable, a
single owner, and a boundary that can be stated in one sentence: *the Postfix queue on VM 108*,
*the `resources` plugin's proto and its generated code*, *the Changelog row for this change*.
If you cannot name the deliverable, you are not ready to launch.

**Independent workstreams run at the same time.** `background: true`, all of them, in one
batch. Diagnosis of stack A and diagnosis of stack B do not wait for each other, and neither
does a document rewrite that depends on findings the orchestrator already holds.

**The split is decided before anything launches.** Write down, in the orchestrator's own
message, which workstream does what and which files each one owns. A split invented halfway
through, reactively, is how two subagents end up in the same file.

**The orchestrator never works on a file a running subagent owns.** Not to "save time", not to
"just fix the header". Files are the collision boundary; respect it while the subagent runs,
and edit only after it reports and its claim has been verified.

**Work that collides is chained, not parallelised.** Two workstreams that must both write
`~/AGENTS.md`, or both edit the same terraform file, or both run `make install` against the
same daemon, are one workstream or two sequential ones. Same for anything that restarts a
shared service: a restart is a global side effect, not a local edit.

## The brief

A subagent starts with no context beyond what the brief carries. A brief that needs a paragraph
of history to make sense is the orchestrator's failure, not the subagent's: it means the
orchestrator knew something it did not write down.

What every brief carries:

- **The machine, and how to reach it.** Laptop? `ssh Homelab`? `ssh ServerNoTanGordo`? Say
  which, and say which identity/docker context applies if it matters.
- **The files or directories it owns.** Paths, and read-only for everything else.
- **The facts already measured, each with its source.** So it does not re-derive them. The
  source matters: a subagent that is told *"Prometheus is on 9090"* will test it, a subagent
  that is told *"Prometheus is on 9090 (source: `docker-compose-services/monitoring/.../compose.yaml`)"*
  will trust it and move on.
- **The exact deliverable and its format.** A file at a path, a report with named sections, a
  YAML diff, a patch. "Investigate X" is not a deliverable.
- **The constraints.** Do not commit, do not restart, do not sudo, do not touch that other
  thing. Explicit, because the default behaviour of a capable subagent is to be helpful and
  finish the job, including the parts you wanted to keep.
- **What "done" looks like.** An observable state, and ideally the command that shows it.

Template, copy and fill:

```
MACHINE
  <laptop: this repo, $HOME is the repo root | VM 108: ssh Homelab | Proxmox host: ssh ServerNoTanGordo>
  reach it as: <ssh target / local shell / docker context>

YOU OWN
  write: <path or directory, one line each>
  read-only everywhere else.

ALREADY MEASURED - do not re-derive
  - <fact>    <- source: <command, or path:line, or log line>
  - <fact>    <- source: <...>

DELIVERABLE
  <exact artifact and format: file at <path> | report with sections A/B/C | unified diff | YAML block>
  <where to write it, or that it comes back in your reply>

CONSTRAINTS
  - do not commit, do not push
  - do not restart containers or services
  - no sudo (and on the laptop, no `sudo` at all - `exec_run` with `sudo: true` if it is needed)
  - do not touch <other file, other stack, other machine>

DONE MEANS
  <observable state>, shown by: <the command that shows it>
```

Keep it short. If the brief is longer than the work, the work was not decomposed well enough
yet.

## What never delegates

Two duties stay with the orchestrator, always.

**The verification.** A subagent's report is a claim, not a measurement. Re-measure it before
repeating it to the operator: query the metric, open the endpoint, read the file, `git log`.
This is not distrust of the subagent, it is the difference between *"a container was
restarted"* and *"the container is up and serving"*. The orchestrator is the only party whose
report leaves the machine, so the orchestrator is the only party that has to be right.

**The record.** The issue, the document, the commit. Whoever writes the durable artifact has to
hold the whole picture, and a subagent holds one workstream by construction. A commit written
by a subagent describes its own slice and cannot see the other three.

The reason is blunt: **a subagent that says "done" plus an orchestrator that repeats it is how a
wrong sentence acquires authority.** Once it is in the operator's head, or in the Changelog, or
in the commit message, it is a fact about the estate that nobody measured.

## Anti-patterns, and what each one cost

All six are from 2026-10-20, in this repo and in the estate.

**The orchestrator keeps the live work.** Unmounting filesystems, restarting containers,
running the fix while the subagents do the reading. It feels efficient because the orchestrator
already has the context. It ends with the operator telling the orchestrator off for doing
precisely what rule 23 forbids, on the task that rule 23 was written from. The context that
made the orchestrator feel qualified is the same context it needs to verify the outcome, and it
is the first thing a live command destroys.

**A subagent asked to "apply the patch" also validates it.** One agent wrote the change and
confirmed the change, so a single wrong assumption appears once and looks verified. Nothing
contradicts it because nothing independent looked at it. Split write from check: the author
produces the artifact, the verifier starts from the artifact and the acceptance test, and the
orchestrator owns the decision between them.

**Two subagents writing to the same repository at once.** Two `git add`, two commits, an
interleaved index, and one workstream staging another's in-flight file. The fix is upstream of
the collision: decide file ownership in the split, and chain anything that must share a tree.
Note that in this repo the tree routinely carries other agents' work, so "stage only what this
change touched" is not paranoia, it is the only way `git status --porcelain` stays readable.

**Accepting a report that says a thing works, without the command that proves it.** "The stack
is healthy, metrics are flowing" with no `up{...}` value, no HTTP status, no `systemctl
is-active` output. The claim may well have been true when it was written. It is not evidence
now, and the orchestrator repeating it makes it the estate's official position.

**Polling or duplicating a running subagent's task.** Re-running the query "to check", sending a
follow-up that repeats the brief, or starting a second agent on the same question because the
first was quiet. It burns the same quota twice, and when the two answers differ the
orchestrator has created a disagreement it now has to adjudicate. Wait for the report. If it
never comes, that is the finding.

**Treating a silent subagent as a success.** Two tasks were delegated in one session and neither
ever reported: a correction comment on an issue that was never posted, and an issue that was
never filed. The orchestrator moved on assuming both were done. The tell was the absence of a
report, and nobody noticed until a later task happened to reveal the gap. The cost is not the
lost work. It is that the *record* silently disagrees with the estate, and a wrong record is
worse than a missing one because it is confidently wrong. **A subagent that does not report is a
task whose outcome you do not know - not a task that succeeded.** Every delegated write (a
comment, an issue, a document, a commit, a file) is verified by reading it back, the same way a
metric is queried: `get_issue_by_index` for the comment count and the body, `git log` for the
commit, a `GET` on the document for the text. Do it as part of closing the workstream, not when
something later looks odd. And when a subagent does report, its report is still a claim:
re-measure it.

## Verification recipes

Cheap, one line each. Run them yourself; do not delegate these. Replace names before use, and
if you do not know a port, read it from the stack's `compose.yaml` rather than guessing.

```sh
# A metric actually has a value now (prometheus MCP `query`, or the HTTP API)
curl -s 'http://127.0.0.1:9090/api/v1/query' --data-urlencode 'query=up{job="node"}' | head -c 400
# better: the prometheus MCP tool `query`, same expression, returns typed JSON

# An endpoint answers at all - status code only, no body, no TLS noise on a self-signed host
curl -sk -o /dev/null -w '%{http_code}\n' https://litellm.visitmy.app
curl -sk -o /dev/null -w '%{http_code} %{time_connect}\n' https://192.168.0.29:47990/

# A unit is running on this laptop
systemctl is-active pangolin-client.service
systemctl --user is-active dotfilesd.service

# The commit is the one you think it is, and the tree is only what you staged
git log -1 --format='%h %s'
git status --porcelain

# A provisioning/compose YAML parses before anything trusts it
docker compose -f docker-compose-services/<stack>/compose.yaml config -q
python -c 'import sys,yaml; yaml.safe_load(open(sys.argv[1])); print("ok")' <file>.yaml

# A git push reached both URLs (origin has two push URLs; one push, two results)
git push --dry-run 2>&1 | tail -5
```

Rules for reading the output:

- **A number with no timestamp is not a measurement.** `up == 1` for a job that stopped being
  scraped yesterday is a stale series. Check freshness, or ask for a range.
- **An HTTP 200 is not a working service.** It is a listener. Whether it is the *right* listener
  is the next question, and that one is usually answered by the body or by the page title.
- **`git status --porcelain` before and after staging**, always. It is the only cheap proof that
  nothing else got swept into the commit.
- **A successful push is two lines, not one.** Confirm both URLs, not just the exit code.

## Where it is also written down

- **This laptop:** rule 23 of `/home/manu343726/AGENTS.md`, the dotfiles contract.
- **The estate:** `~/AGENTS.md` on VM 108, section *"How an agent works here: the session is an
  orchestrator"*. It carries the same rule for homelab work; this skill is the operational
  detail for both. Read it there (`ssh Homelab 'cat ~/AGENTS.md'`) rather than trusting this
  summary, and treat the estate's wording as the source for estate work.
