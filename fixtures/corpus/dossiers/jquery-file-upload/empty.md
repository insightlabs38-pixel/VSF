# jQuery File Upload demo - empty
Upstream: https://github.com/blueimp/jQuery-File-Upload
Framework: jquery-bootstrap-3
Ground truth basis: jQuery File Upload documentation and demo description; Chromium accessibility tree
Page title: jQuery File Upload Demo
State: as loaded
What people do here (from the manifest):
- choose files to upload

## Accessibility tree (Chromium, full page)
```yaml
- list:
  - listitem:
    - link "Project":
      - /url: https://github.com/blueimp/jQuery-File-Upload
  - listitem:
    - link "Demo":
      - /url: "#"
  - listitem:
    - link "Wiki":
      - /url: https://github.com/blueimp/jQuery-File-Upload/wiki
  - listitem:
    - link "Author":
      - /url: https://blueimp.net
- heading "jQuery File Upload Demo" [level=1]
- blockquote:
  - paragraph: File Upload widget with multiple file selection, drag&drop support, progress bars, validation and preview images, audio and video for jQuery. Supports cross-domain, chunked and resumable file uploads and client-side image resizing. Works with any server-side platform (PHP, Python, Ruby on Rails, Java, Node.js, Go etc.) that supports standard HTML form file uploads.
- text: + Add files...
- button "Choose File"
- button " Start upload"
- button " Cancel upload"
- button " Delete selected"
- checkbox
- progressbar
- heading "Demo Notes" [level=3]
- list:
  - listitem:
    - text: The maximum file size for uploads in this demo is
    - strong: 999 KB
    - text: (default file size is unlimited).
  - listitem:
    - text: Only image files (
    - strong: JPG, GIF, PNG
    - text: ) are allowed in this demo (by default there is no file type restriction).
  - listitem:
    - text: Uploaded files will be deleted automatically after
    - strong: 5 minutes or less
    - text: (demo files are stored in memory).
  - listitem:
    - text: You can
    - strong: drag & drop
    - text: files from your desktop on this webpage (see
    - link "Browser support":
      - /url: https://github.com/blueimp/jQuery-File-Upload/wiki/Browser-support
    - text: ).
  - listitem:
    - text: Please refer to the
    - link "project website":
      - /url: https://github.com/blueimp/jQuery-File-Upload
    - text: and
    - link "documentation":
      - /url: https://github.com/blueimp/jQuery-File-Upload/wiki
    - text: for more information.
  - listitem:
    - text: Built with the
    - link "Bootstrap":
      - /url: https://getbootstrap.com/
    - text: CSS framework and Icons from
    - link "Glyphicons":
      - /url: https://glyphicons.com/
    - text: .
```
