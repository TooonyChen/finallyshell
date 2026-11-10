# Mockups

Read this before showing the owner what a UI change will look like (step
1 of the workflow). A mockup is a picture of the finished feature, made
before any code, so the owner can say "yes, that" or "no, move this"
while it costs nothing to change.

## What to draw

- The part of the window the change is in, at the app's real size and
  look, with enough around it to place it (the command bar above a tab,
  the session list beside a panel). Not the whole window unless the
  change is the layout.
- Realistic content: commands, host names and paths a user would have,
  not "lorem ipsum" or "Button 1".
- Numbered red callouts on the new parts, and a legend under the window
  that says what each one does and why. The legend carries the
  decisions: defaults, what is kept or saved, what is shared with
  existing features.
- When there is a real choice (two layouts, a menu or a drag), draw both
  side by side with a short pro and con under each, and the
  recommendation.
- When the change follows another app (FinalShell, Xshell), put a crop of
  that app's screenshot next to it, the relevant part framed.
- In the owner's language (Simplified Chinese for this owner), with the
  app's own wording from `locales/zh-Hans.json` for existing labels.

## Making it

1. Write an HTML page in the scratchpad. Copy the look from
   `docs/screenshots/*.png` (open them with Read): light theme, sidebar
   gray `#f3f4f6`, borders `#e5e7eb`/`#d1d5db`, accent blue `#2563eb`,
   warning orange `#c2410c` on `#fff7ed`, the terminal `#1e1f22`, 13 px
   UI text, a monospace font for commands, 6 px radius on buttons. Plain
   CSS, no frameworks; flex and grid are enough. Size the `<body>` with a
   width (about 1240 px) and padding: the screenshot is of the body.
2. Put callouts inline next to the element they mark (a small red circle
   in the same row), not absolutely positioned: absolute ones drift onto
   the wrong element when the layout changes.
3. Render it: `node .claude/skills/issue-workflow/references/render-mockup.js page.html`
   writes `page.png` at 2x. CJK text needs a CJK font; the sandbox has
   WenQuanYi Zen Hei (`fc-list :lang=zh`), put it in the `font-family`
   list after the macOS ones.
4. Look at the PNG with Read before anyone else does. Fix text that
   overlaps, is cut off or wraps badly, menus that cover what they
   explain, callouts on the wrong thing. Render again. Usually two or
   three rounds.
5. Crops of other screenshots: Python with Pillow (`Image.crop`,
   `ImageDraw.rectangle` for the frame), saved at 2x.

## Showing it and keeping it

- Send the PNGs to the owner with `SendUserFile` (display `render`), with
  a short note per image, and the questions that are theirs to answer.
  Change the mockup until they approve it.
- After approval, put the images on the `chore/mockups` branch, which
  has no history in common with `main` and is never merged:
  `<issue>/<name>.png`, lowercase with hyphens. Commit them without
  touching the working tree, with a temporary index:

  ```bash
  export GIT_INDEX_FILE=$SCRATCH/mockups.index
  git fetch origin chore/mockups
  git read-tree origin/chore/mockups
  h=$(git hash-object -w path/to/mockup.png)
  git update-index --add --cacheinfo 100644,$h,164/local-terminal.png
  t=$(git write-tree); unset GIT_INDEX_FILE
  c=$(git commit-tree $t -p origin/chore/mockups -m "chore: add the mockup of #164")
  git push origin $c:refs/heads/chore/mockups
  ```

  (Add the session's attribution trailers to the message.) Never
  `git checkout --orphan` in a working tree: `git rm` refuses the staged
  files and a following commit takes the whole repository.
- Embed it in the issue body under `## Mockup`, or in a comment on an
  issue that already exists, with the blob link, which works for anyone
  who can see the repository:

  ```markdown
  ![Command editor](https://github.com/TooonyChen/finallyshell/blob/chore/mockups/160/command-editor-mockup.png?raw=true)
  ```

  GitHub's own attachment upload (drag and drop) works only in a
  browser, which agents don't have.
- Write what the mockup shows in the issue too (the legend's points), so
  the issue reads without the image.
- Give the subagent that builds it the local path and the link, and ask
  it to compare its tester screenshots with the mockup in the PR.
