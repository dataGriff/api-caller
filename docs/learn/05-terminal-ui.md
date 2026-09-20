# Lesson 5: The terminal UI tour

**Goal.** Drive a whole project from `apic ui` without touching the
command line: run, read, check, edit, reload.

## You will need

- apic installed ([lesson 1](01-first-request.md))
- `apic demo` running in another terminal, or start it now:

```sh
apic demo --out apic-demo
```

- A terminal at least 70 columns by 16 rows. The UI says so if it is
  smaller.

This lesson has no commands to copy after the first one: every step is a
key. Keep the page beside the terminal.

## Steps

### 1. Open it

```sh
apic ui -C apic-demo
```

![apic ui on the demo project](../assets/apic-ui.svg){ .apic-shot }

Four areas. **Requests** on the left, grouped by file, one row per
request. On the right, a tab strip with **preview**, **response**,
**checks** and **session**. The status bar at the bottom shows the project
root, the environment, and the result of the last run. Nothing is
different from the command line underneath: the same runner, the same
session file, the same environment files. What you see here is what CI
sees.

If it refuses to start, that is by design: `apic ui` is the one
interactive command, and it declines a pipe or `--json` rather than leave
a script waiting on a screen. Everything else in apic is non-interactive.

### 2. Read the marks

Every row starts with a mark:

| Mark | Meaning |
|---|---|
| `●` | Ready: every variable it needs resolves |
| `○` | Not ready: something is missing, usually a token another request captures |
| `✓` | Ran and passed every assertion and capture |
| `✗` | Ran and failed, or could not be sent |
| spinner | In flight |

On a fresh session most of `todos.http` is `○`: those requests need
`{{token}}` and nothing has captured one yet. `whoami` is `●` even so,
because its `# @ref login` promises to fetch it. Move the cursor with
<kbd>j</kbd> and <kbd>k</kbd> (or the arrows) and watch the **preview** tab
follow: it is `apic describe` for the selected request, with every
variable and where it came from, and the missing ones called out with the
request that would provide them.

### 3. Run one

Put the cursor on `login` and press <kbd>enter</kbd>. The row shows a
spinner, then `✓ 200 1ms`, and the right pane switches to the **response**
tab: status line, body pretty-printed and highlighted, the assertion that
passed, the value captured. Look left again: every `○` in `todos.http` is
now `●`. The token landed in the session and the marks updated without a
reload. That is [lesson 3](03-capture-session-flows.md) happening in
front of you.

Press <kbd>H</kbd> to add the request and response headers to the
response tab, and again to hide them. Press <kbd>c</kbd> to swap the
response for the equivalent `curl` command, ready to paste; <kbd>c</kbd>
again brings the response back.

### 4. Run a file

Move down into `todos.http` and press <kbd>f</kbd>: run every request in
the file the cursor is in, in order, as a flow. The rows light up one after
another, each turning `✓` or `✗` as its response lands, and the file
heading rolls them up: `todos.http ✓11 ✗1`. The `✗` is `not-found`, the
request that fails on purpose; the flow carries on past it in the UI so
you can see the whole file.

The status bar says `last: 1 failed, 11 passed`. <kbd>a</kbd> does the
same for the whole project.

### 5. Read a failure

With the cursor on `not-found`, press <kbd>3</kbd> for the **checks** tab.
It lists each assertion with its actual and expected values side by side:

```
✗ status == 200
    actual   404
    expected 200
```

For a long response, <kbd>J</kbd> and <kbd>K</kbd> scroll the right pane a
line at a time, <kbd>ctrl+d</kbd> and <kbd>ctrl+u</kbd> half a page, and
the right end of the tab strip says where you are: `top ↓`, `↑ 40% ↓`,
`↑ end`. Scrolling stays put when a later step of a flow lands underneath
you; moving to another request starts at the top again.

### 6. The session

Press <kbd>4</kbd>. The **session** tab lists the values captured for the
current environment: `token`, `todoId`, `todoTitle`, exactly what
`apic session` prints and what the next `apic run` will use. <kbd>x</kbd>
clears them, after a confirmation. Do it, and watch `todos.http` go back
to `○`. Then press <kbd>enter</kbd> on `login` again.

### 7. Find, switch, edit, reload

- <kbd>/</kbd> filters the list as you type, by id, URL, method, file or
  description. Type `todo`, press <kbd>enter</kbd> to keep the filter, and
  <kbd>esc</kbd> to clear it.
- <kbd>e</kbd> switches to the next environment. The project is rebuilt
  with that environment's variables and session, and the results on screen
  are cleared, because they came from somewhere else. If you did lesson 2's
  exercise, `staging` is one press away.
- <kbd>o</kbd> opens the selected request's file at its line in `$VISUAL`,
  then `$EDITOR` (`vi` if neither is set). Save and quit, and the project
  is reloaded: a new request appears in the list, a changed assertion runs
  next time. <kbd>r</kbd> reloads without editing, for files changed by
  anything else.
- <kbd>?</kbd> shows every key. <kbd>q</kbd> quits.

Started with `--redact`, the UI masks bodies, header values, query values
and captures on screen exactly as `apic run --redact` does, and shows a
`redact` badge in the status bar so a screenshot never hides that values
were masked. `--demo` shows a `demo` badge the same way.

## Checkpoint

Press <kbd>g</kbd> to go to the top, move onto any request in
`todos.http`, and press <kbd>f</kbd>. When the flow finishes, the file
heading reads `todos.http ✓11 ✗1`, the `✗` row is `not-found`, and the
status bar says `last: 1 failed, 11 passed`. If it does, you have run,
read and checked a flow without leaving the UI.

## Exercise

Without quitting the UI, give `health` in `explore.http` a fourth
assertion, `body.$.service == apic-demo`, and see it pass.

??? example "Solution"
    Move the cursor onto `health`, press <kbd>o</kbd>, and the file opens
    at line 7 in your editor. Add the line under the other assertions:

    ```http
    # @assert body.$.version exists
    # @assert body.$.service == apic-demo
    GET {{baseUrl}}/health
    ```

    Save and quit. The UI reloads the project. Press <kbd>enter</kbd> on
    `health`, then <kbd>3</kbd>: four checks, four `✓`. If your editor is
    not what you expected, set `EDITOR` before starting the UI, for example
    `EDITOR=nano apic ui -C apic-demo`.

## Going further

- [The terminal UI](../tui.md), every key and how it behaves
- [`apic ui`](../cli.md#apic-ui) in the reference, including why it refuses pipes

??? note "Episode script"
    **Length.** 8 minutes, almost entirely screen.

    **Cold open (0:00).** The UI opening on the demo, cursor on `login`,
    `todos.http` all `○`. "Every request in the project, and whether it
    can run right now. Watch what one key does."

    **Talking points.**

    1. The four areas and the marks. Move the cursor; the preview follows.
    2. <kbd>enter</kbd> on `login`: the response tab, and the `○` marks
       flipping to `●` on the left. Pause on that.
    3. <kbd>H</kbd> and <kbd>c</kbd> on the response.
    4. <kbd>f</kbd> on `todos.http`: rows lighting up in order, the file
       heading rolling up `✓11 ✗1`.
    5. <kbd>3</kbd> on `not-found`: actual versus expected.
    6. <kbd>4</kbd>, <kbd>x</kbd>, marks back to `○`, `login` again.
    7. <kbd>/</kbd>, <kbd>e</kbd>, <kbd>o</kbd> with the exercise edit
       live, <kbd>r</kbd>, <kbd>?</kbd>.
    8. `--redact` badge; why it refuses pipes and `--json`.

    **Shot list.** One terminal, 112x30, font size 16, nothing else on
    screen. `docs/learn/tapes/05.tape` drives the whole episode; it is
    `docs/assets/demo.tape` extended with the edit round trip.

    **Chapters.** `0:00 One key` · `0:40 The screen and the marks` · `1:40
    Run one` · `2:40 Run a file` · `3:50 Read a failure` · `4:50 The
    session` · `5:40 Find, switch, edit, reload` · `7:10 Checkpoint and
    exercise`.

    **Description.** From the [episode template](_episode-template.md).
