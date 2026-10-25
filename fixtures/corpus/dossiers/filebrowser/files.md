# File Browser - files
Upstream: https://github.com/filebrowser/filebrowser
Framework: vue
Ground truth basis: File Browser documentation (users, sharing, uploads, downloads, commands); Chromium accessibility tree
Page title: My files - Files - File Browser
State: signed in, file listing
What people do here (from the manifest):
- browse files and folders
- open a folder

## Accessibility tree (Chromium, full page)
```yaml
- heading "File Browser" [level=1]
- navigation:
  - button "person admin"
  - button "My files": folder My files
  - button "New folder": create_new_folder New folder
  - button "New file": note_add New file
  - button "Settings": settings_applications Settings
  - button "Logout": exit_to_app Logout
  - text: 13 GiB of 252 GiB used
  - paragraph:
    - link "File Browser":
      - /url: https://github.com/filebrowser/filebrowser
    - text: 2.63.23 Help
- main:
  - link "Home":
    - /url: /files
    - text: home
  - img "File Browser"
  - text: search
  - textbox "Search..."
  - paragraph: Type to search...
  - heading "Types" [level=3]
  - button "Images":
    - text: insert_photo
    - paragraph: Images
  - button "Music":
    - text: volume_up
    - paragraph: Music
  - button "Video":
    - text: movie
    - paragraph: Video
  - button "PDF":
    - text: picture_as_pdf
    - paragraph: PDF
  - button "Switch view": view_module
  - button "Download": file_download
  - button "Upload": file_upload
  - button "Info": info
  - button "Select multiple": check_circle
  - button "Sort by name": Name arrow_upward
  - button "Sort by size": Size arrow_upward
  - button "Sort by last modified": Last modified arrow_upward
  - button "docs":
    - text: folder
    - paragraph: docs
    - paragraph: —
    - paragraph:
      - time: a few seconds ago
  - button "photos":
    - text: folder
    - paragraph: photos
    - paragraph: —
    - paragraph:
      - time: a few seconds ago
  - button "data.csv":
    - text: border_all
    - paragraph: data.csv
    - paragraph: 12 B
    - paragraph:
      - time: a few seconds ago
  - button "readme.txt":
    - text: description
    - paragraph: readme.txt
    - paragraph: 6 B
    - paragraph:
      - time: a few seconds ago
  - paragraph: Multiple selection enabled
  - button "Clear": clear
```
