# Episode template

The YouTube description, title and thumbnail conventions for the "From
zero to apic" series. Every lesson's episode is cut from this so the
series reads as one thing.

## Title

`From zero to apic #N: <lesson title>` for the numbered lessons, and
`From zero to apic: <topic>` for the occasional extra. Keep it under 60
characters so it survives the mobile crop.

## Thumbnail

The apic logo, the episode number, three to five words of the lesson
title, and one screenshot from the episode. The docs theme's purple on the
dark window background, so a row of thumbnails looks like one series.

## Description

Fill in every `<…>`. Keep the first line under 120 characters; it is the
line search results show.

```text
<One sentence: what you can do after this episode.>

This is lesson <N> of From zero to apic, a course on running API requests
from plain .http files in the terminal, in CI and from AI agents, with one
static binary. Every lesson runs against the fake API that ships inside
apic, so you can follow along with nothing but the binary.

📄 Lesson page, with every command to copy:
https://datagriff.github.io/api-caller/learn/<NN-slug>/

▶ Try it in 30 seconds:
    go install github.com/dataGriff/api-caller/cmd/apic@latest
    apic ui --demo

Chapters
0:00 <Why>
<m:ss> <Step 1>
<m:ss> <Step 2>
<m:ss> Checkpoint
<m:ss> Exercise
<m:ss> Next lesson

Links
🏠 Docs: https://datagriff.github.io/api-caller/
📦 Source and issues: https://github.com/dataGriff/api-caller
📚 The whole course: https://datagriff.github.io/api-caller/learn/
⬅ Previous: <link or "this is the first one">
➡ Next: <link or "coming soon">

apic is MIT licensed and built in the open. If something in this episode
does not work for you, the lesson page has a "Report a problem" link at
the bottom; the commands on it are checked in CI, so it is probably a
version difference, and the page says which version it was written for.
```

## Recording notes

- One terminal, 100 columns by 30 rows, font size 16, dark background.
  The lesson's `docs/learn/tapes/NN.tape` (a [VHS](https://github.com/charmbracelet/vhs)
  script) reproduces the terminal parts on demand, so b-roll can be
  re-recorded without re-doing the voice.
- Say the command before typing it, then pause on the output for a beat
  longer than feels natural. Viewers read slower than you talk.
- Every episode ends on the checkpoint passing, then the one-line pitch
  for the next lesson.
