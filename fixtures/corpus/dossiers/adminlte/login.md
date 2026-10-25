# AdminLTE 4 admin dashboard template - login
Upstream: https://github.com/ColorlibHQ/AdminLTE
Framework: bootstrap-5
Ground truth basis: AdminLTE page descriptions; Chromium accessibility tree
Page title: AdminLTE 4 | Login Page
State: as loaded
What people do here (from the manifest):
- sign in with email and password

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
  - paragraph: Sign in to start your session
  - text: Email
  - textbox "Email"
  - text:  Password
  - textbox "Password"
  - text: 
  - checkbox "Remember Me"
  - text: Remember Me
  - button "Sign In"
  - paragraph: "- OR -"
  - link " Sign in using Facebook":
    - /url: "#"
  - link " Sign in using Google":
    - /url: "#"
  - paragraph:
    - link "I forgot my password":
      - /url: forgot-password.html
  - paragraph:
    - link "Register a new membership":
      - /url: register.html
- status
```
