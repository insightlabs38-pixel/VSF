# jQuery UI demos - checkboxradio-default
Upstream: https://github.com/jquery/jquery-ui
Framework: jquery
Ground truth basis: jQuery UI demo descriptions and API documentation; Chromium accessibility tree
Page title: jQuery UI Checkboxradio - Default functionality
State: as loaded
What people do here (from the manifest):
- choose a hotel rating and bed type with styled radios and checkboxes

## Accessibility tree (Chromium, full page)
```yaml
- heading "Checkbox and radio button widgets" [level=1]
- heading "Radio Group" [level=2]
- group "Select a Location:":
  - text: "Select a Location: New York"
  - radio "New York"
  - text: Paris
  - radio "Paris"
  - text: London
  - radio "London"
- heading "Checkbox" [level=2]
- group "Hotel Ratings:":
  - text: "Hotel Ratings: 2 Star"
  - checkbox "2 Star"
  - text: 3 Star
  - checkbox "3 Star"
  - text: 4 Star
  - checkbox "4 Star"
  - text: 5 Star
  - checkbox "5 Star"
- heading "Checkbox nested in label" [level=2]
- group "Bed Type:":
  - text: "Bed Type: 2 Double"
  - checkbox "2 Double"
  - text: 2 Queen
  - checkbox "2 Queen"
  - text: 1 Queen
  - checkbox "1 Queen"
  - text: 1 King
  - checkbox "1 King"
- paragraph: Examples of the markup that can be used with checkboxes and radio buttons.
```
