# AdminLTE 4 admin dashboard template - register
Upstream: https://github.com/ColorlibHQ/AdminLTE
Framework: bootstrap-5
Ground truth basis: AdminLTE page descriptions; Chromium accessibility tree
Page title: AdminLTE 4 | Register Page
State: as loaded
What people do here (from the manifest):
- register a new account

## Accessibility tree (Chromium, full page)
```yaml
- link "Skip to main content":
  - /url: "#main"
- link "Skip to navigation":
  - /url: "#navigation"
- main:
  - heading "AdminLTE" [level=1]:
    - link "AdminLTE":
      - /url: ../index2.html
  - paragraph: Register a new membership
  - text: Full Name
  - textbox "Full Name"
  - text:  Email
  - textbox "Email"
  - text:  Password
  - textbox "Password"
  - text: 
  - checkbox "I agree to the terms"
  - text: I agree to the
  - link "terms":
    - /url: "#"
  - button "Register"
  - paragraph: "- OR -"
  - link " Sign up using Facebook":
    - /url: "#"
  - link " Sign up using Google":
    - /url: "#"
  - paragraph:
    - link "I already have a membership":
      - /url: login.html
- status
```
