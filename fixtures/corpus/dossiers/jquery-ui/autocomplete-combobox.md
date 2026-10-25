# jQuery UI demos - autocomplete-combobox
Upstream: https://github.com/jquery/jquery-ui
Framework: jquery
Ground truth basis: jQuery UI demo descriptions and API documentation; Chromium accessibility tree
Page title: jQuery UI Autocomplete - Combobox
State: as loaded
What people do here (from the manifest):
- choose a programming language with a custom combobox

## Accessibility tree (Chromium, full page)
```yaml
- text: "Your preferred programming language:"
- textbox
- button "Show All Items"
- button "Show underlying select"
- paragraph: A custom widget built by composition of Autocomplete and Button. You can either type something into the field to get filtered suggestions based on your input, or use the button to get the full list of selections.
- paragraph: The input is read from an existing select-element for progressive enhancement, passed to Autocomplete with a customized source-option.
- paragraph:
  - text: This is not a supported or even complete widget. Its purely for demoing what autocomplete can do with a bit of customization.
  - link "For a detailed explanation of how the widget works, check out this Learning jQuery article.":
    - /url: https://www.learningjquery.com/2010/06/a-jquery-ui-combobox-under-the-hood
- status
- log
- log
```
