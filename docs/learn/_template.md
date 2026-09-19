# Writing a lesson

This page is the template every lesson in the course is copied from, and
the rules a lesson follows. Copy it to `docs/learn/NN-slug.md`, replace
each section, and add the page to the **Learn** tab in `mkdocs.yml`.

## The rules

- **Second person, present tense.** "You run", not "we will run".
- **One concept per lesson.** If a step needs a second idea explained,
  that idea is its own lesson, or it belongs in Going further.
- **Every command is copy-pasteable and shows its output.** A reader
  compares what they see with what the page shows; never make them guess.
- **Every step runs against `apic demo`** unless the lesson is explicitly
  about a real API. Nobody gets stuck at a login page.
- **Mark runnable blocks** with an HTML comment on the line before the
  fence, `<!-- learn -->`. CI extracts those blocks, starts `apic demo`
  in a scratch directory, and runs them in order with `sh -e`, so a lesson
  cannot rot. Blocks that need credentials, an editor or a browser are
  left unmarked. Run the check yourself with `task learn:check`.
- **The checkpoint is one command** whose output settles whether the
  lesson took. Make it the last runnable block.
- **The episode script lives on the page**, collapsed at the end, so the
  video and the text never drift apart.

The rest of this page is the template. Its example blocks are real and run
in CI, so the harness itself is tested.

---

# Lesson N: Title in sentence case

**Goal.** One sentence: what you can do when you finish. For example: run a
request from a `.http` file against a local API and read the result.

## You will need

- apic 0.2 or later (`apic version`)
- `apic demo` running in another terminal (lesson 1 shows how), or start it
  now:

<!-- learn -->
```sh
apic demo --out apic-demo --force >/dev/null 2>&1 &
sleep 1
```

!!! note
    The harness runs this block too, so a lesson that starts the demo
    itself works both for a reader and in CI. If the demo is already
    running on the default port, use `--port` and pass the same port in
    `http-client.env.json`.

## Steps

### 1. Do the first thing

Say what the reader is about to do and why, in a sentence. Then the
command:

<!-- learn -->
```sh
apic list -C apic-demo
```

Then what they should see. Show the real output, trimmed to the lines that
matter:

```
ID              METHOD  URL                        LINE  DESCRIPTION
auth.http
login           POST    {{baseUrl}}/auth/login     :5    Log in and keep the token
whoami          GET     {{baseUrl}}/me             :15   Who am I, using the token captured by login
```

### 2. Do the next thing

Each step ends with something the reader can see change. Point at it: "the
row for `whoami` now shows `●`, because the token it needs was captured a
moment ago."

<!-- learn -->
```sh
apic run login -C apic-demo
```

## Checkpoint

One command, one expected result. If it matches, the lesson took.

<!-- learn -->
```sh
apic run whoami -C apic-demo --json | grep -o '"ok":true'
```

```
"ok":true
```

## Exercise

One task that uses what the lesson taught with a small twist. No new
concepts.

??? example "Solution"
    The solution, as a `.http` snippet or a command, with a line on why it
    works.

## Going further

- The guide page this lesson skimmed, for example [the `.http` format](../format.md)
- The reference section, for example [`apic run`](../cli.md#apic-run)

??? note "Episode script"
    **Length.** 8 to 12 minutes.

    **Cold open (0:00).** The one-line problem this lesson solves, shown
    not said: the error, the manual step, the thing that does not work yet.

    **Talking points.**

    1. Step 1, in the words a viewer hears.
    2. Step 2.
    3. The checkpoint and what it proves.

    **Shot list.** One terminal, 100x30, font size 16, the docs theme.
    `docs/learn/tapes/NN.tape` drives it for b-roll when a recording is
    needed. Zoom on the line that changed after each step.

    **Chapters.** `0:00 Why` · `0:45 Step 1` · `3:10 Step 2` ·
    `6:00 Checkpoint` · `7:30 Exercise` · `8:40 Next lesson`.

    **Description.** From the [episode template](_episode-template.md).
