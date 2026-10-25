# jQuery UI demos - dialog-modal-form
Upstream: https://github.com/jquery/jquery-ui
Framework: jquery
Ground truth basis: jQuery UI demo descriptions and API documentation; Chromium accessibility tree
Page title: jQuery UI Dialog - Modal form
State: modal form opened
What people do here (from the manifest):
- create a new user through a modal form dialog

## Accessibility tree (Chromium, full page)
```yaml
- heading "Existing Users:" [level=1]
- table:
  - rowgroup:
    - row "Name Email Password":
      - cell "Name"
      - cell "Email"
      - cell "Password"
  - rowgroup:
    - row "John Doe john.doe@example.com johndoe1":
      - cell "John Doe"
      - cell "john.doe@example.com"
      - cell "johndoe1"
- button "Create new user"
- paragraph:
  - text: Use a modal dialog to require that the user enter data during a multi-step process. Embed form markup in the content area, set the
  - code: modal
  - text: option to true, and specify primary and secondary user actions with the
  - code: buttons
  - text: option.
- dialog "Create new user":
  - text: Create new user
  - button "Close"
  - paragraph: All form fields are required.
  - group:
    - text: Name
    - textbox "Name": Jane Smith
    - text: Email
    - textbox "Email": jane@smith.com
    - text: Password
    - textbox "Password": [ value withheld ]
    - button "Submit"
  - button "Create an account"
  - button "Cancel"
```
