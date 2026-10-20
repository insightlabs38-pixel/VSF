# jQuery UI demos - dialog-modal-confirmation
Upstream: https://github.com/jquery/jquery-ui
Framework: jquery
Ground truth basis: jQuery UI demo descriptions and API documentation; Chromium accessibility tree
Page title: jQuery UI Dialog - Modal confirmation
State: as loaded
What people do here (from the manifest):
- confirm a destructive action in a modal dialog

## Accessibility tree (Chromium, full page)
```yaml
- paragraph:
  - text: Sed vel diam id libero
  - link "rutrum convallis":
    - /url: https://example.com
  - text: . Donec aliquet leo vel magna. Phasellus rhoncus faucibus ante. Etiam bibendum, enim faucibus aliquet rhoncus, arcu felis ultricies neque, sit amet auctor elit eros a lectus.
- paragraph:
  - text: Confirm an action that may be destructive or important. Set the
  - code: modal
  - text: option to true, and specify primary and secondary user actions with the
  - code: buttons
  - text: option.
- dialog "Empty the recycle bin?":
  - text: Empty the recycle bin?
  - button "Close"
  - paragraph: These items will be permanently deleted and cannot be recovered. Are you sure?
  - button "Delete all items"
  - button "Cancel"
```
