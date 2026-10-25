# Operation annotation guide

An operation is one thing a person can safely do on a page state and check afterwards. Operations are written by someone who has not seen what VSF recognizes, from the same dossier and annotation the page already has. They are the only ground truth for executable-operation correctness, so each must be checkable from what the application shows, not from how the page is built.

Output: one JSON file per page, `fixtures/corpus/operations/<source>/<page>.json`, shaped as `src/operations.ts` (`vsf-corpus-operations-v1`). `dossierSha256` is the sha256 of the dossier file exactly as given.

## What qualifies
- Safe on a disposable local copy: adding a todo, filling a form without sending it, sorting, filtering, paging, selecting rows, opening and closing a dialog, switching a tab, choosing an option, queuing a file. Operations that send mail, charge, delete shared data, change a password or leave the machine are not written.
- Observable: the page itself shows the result in text, a value, a checked state, a row count, or the address fragment.
- Repeatable from a fresh load of the page state. Each operation starts from the page as captured.

## Steps
Each step names its target exactly as an item is named in the page annotation (field, action, composite or collection name), never by selector, position or markup.
- `fill`: put `value` in a text field.
- `choose`: pick the option whose visible label is `value` in a choice field.
- `toggle`: set a toggle field to `checked` true or false.
- `click`: press an action or open a composite item by name.
- `upload`: give a file chooser one generated file; `value` is its name, `text` its content.
- `press`: press `key` (for example `Enter`) with the previous step's target focused.

## Expectations
At least one per operation, all checked after the last step by reading the page, not the recognizer.
- `text`: `contains` is visible somewhere on the page.
- `absent`: `contains` is no longer visible.
- `value`: the field has `value`.
- `checked`: the toggle field is `checked`.
- `rows`: the collection has `count` data rows.
- `fragment`: the address fragment or query contains `contains`.

## Rules
Write only operations that the page state and the application's documented behaviour support. If unsure what happens afterwards, leave the operation out. Record `annotator.kind` as `separate-agent`, `annotator.id` as given, `sawRecognizerOutput` false, and `basis` listing what you used. Where an operation has an obvious variant that must change nothing (an empty entry, a disabled control), add it with `expect` describing no change and `negative: true`.

## File shape
```json
{
  "schema": "vsf-corpus-operations-v1",
  "source": "<source id>", "page": "<page id>", "dossierSha256": "<sha256 of the dossier file>",
  "annotator": { "kind": "separate-agent", "id": "<given id>", "sawRecognizerOutput": false, "basis": ["what you used"] },
  "operations": [
    { "name": "Add a todo", "effect": "state_change",
      "steps": [ { "do": "fill", "target": "New Todo Input", "value": "Buy milk" }, { "do": "press", "key": "Enter" } ],
      "expect": [ { "kind": "text", "contains": "Buy milk" } ] }
  ]
}
```
`effect` is one of read, state_change, destructive, navigation, unknown. Step shapes: `{do:"fill",target,value}`, `{do:"choose",target,value}`, `{do:"toggle",target,checked}`, `{do:"click",target}`, `{do:"upload",target,value,text}`, `{do:"press",key}`. Expectation shapes: `{kind:"text"|"absent"|"fragment",contains}`, `{kind:"value",target,value}`, `{kind:"checked",target,checked}`, `{kind:"rows",target,count}`.

## Checking your own work
`node --conditions=source tests/corpus/src/cli.ts validate` checks shapes and that every target is an item named in the page annotation. `node --conditions=source tests/corpus/src/cli.ts reference --partition <dev|validation|heldout> --only <source>` runs each operation on the live application through Chromium's accessibility locators and prints, per operation, whether it was confirmed, was vacuous (its expectation already held) or was unconfirmed with the reason. Use it to repair or remove operations. It never runs the recognizer. Keep at most four operations per page, preferring distinct behaviours over variants; an unconfirmed operation is dropped from scoring and counted.
