; LANcast Windows installer (ADR 0022). Lays down both executables, registers the
; server as a service, and puts a Start-menu/desktop shortcut to the launcher.
;
; Built in the release pipeline with makensis; VERSION is passed in:
;   makensis -DVERSION=0.3.0 lancast.nsi
; It expects lancastd.exe and lancast.exe beside this script (the release job
; copies them from the goreleaser output).

!include "MUI2.nsh"

!ifndef VERSION
  !define VERSION "0.0.0"
!endif

Name "LANcast"
OutFile "LANcast-Setup-${VERSION}.exe"
Unicode true
InstallDir "$PROGRAMFILES64\LANcast"
; Program Files and service registration both need elevation.
RequestExecutionLevel admin

; Branding: the app icon for the installer, and the L mark on the welcome/finish
; side panel. The panel is only 164px wide, so the wordmark's tagline is
; illegible there — the tagline is set as real text below instead, which stays
; crisp at any DPI.
!define MUI_ICON "..\assets\lancast.ico"
!define MUI_UNICON "..\assets\lancast.ico"
!define MUI_WELCOMEFINISHPAGE_BITMAP "welcome.bmp"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "welcome.bmp"

; The title control is a fixed height and renders large and bold, so anything
; that wraps past two lines is clipped. The name goes there alone; the tagline
; leads the body text, which has room and a smaller font.
!define MUI_WELCOMEPAGE_TITLE "Welcome to LANcast"
!define MUI_WELCOMEPAGE_TEXT "Your gateway to everything on your LAN.$\r$\n$\r$\nSetup will install LANcast on your computer.$\r$\n$\r$\nLANcast runs as a background service and starts with Windows. When Setup finishes, open the LANcast shortcut to get to your library — no terminal required.$\r$\n$\r$\nClick Next to continue."

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "..\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\LANcast-Client.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open LANcast in my browser"
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\LANcast"

Section "LANcast"
  ; Upgrade path. An earlier install registered the service from differently
  ; named executables (lancastd.exe / lancast.exe); leaving that registration
  ; behind would orphan a service pointing at a file this install removes. Stop
  ; and delete any existing service by name first — sc reports an error when the
  ; service is absent, which is fine and ignored.
  nsExec::ExecToLog 'sc.exe stop lancastd'
  nsExec::ExecToLog 'sc.exe delete lancastd'
  Delete "$INSTDIR\lancastd.exe"
  Delete "$INSTDIR\lancast.exe"

  SetOutPath "$INSTDIR"
  File "LANcast-Server.exe"
  File "LANcast-Client.exe"
  File "..\README.md"
  File "..\LICENSE"

  ; Register the server as a service pinned to the machine-wide data dir, and
  ; start it. `service install` refuses an unset --data, so the pin is enforced.
  nsExec::ExecToLog '"$INSTDIR\LANcast-Server.exe" service install'
  nsExec::ExecToLog '"$INSTDIR\LANcast-Server.exe" service start'

  ; Shortcuts point at the launcher, not the daemon.
  CreateDirectory "$SMPROGRAMS\LANcast"
  CreateShortcut "$SMPROGRAMS\LANcast\LANcast.lnk" "$INSTDIR\LANcast-Client.exe"
  CreateShortcut "$DESKTOP\LANcast.lnk" "$INSTDIR\LANcast-Client.exe"

  WriteUninstaller "$INSTDIR\uninstall.exe"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "LANcast"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\LANcast-Client.exe"
  WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "LANcast"
SectionEnd

Section "Uninstall"
  ; Stop and remove the service before deleting its binary.
  nsExec::ExecToLog '"$INSTDIR\LANcast-Server.exe" service stop'
  nsExec::ExecToLog '"$INSTDIR\LANcast-Server.exe" service uninstall'

  Delete "$INSTDIR\LANcast-Server.exe"
  Delete "$INSTDIR\LANcast-Client.exe"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\LANcast\LANcast.lnk"
  RMDir "$SMPROGRAMS\LANcast"
  Delete "$DESKTOP\LANcast.lnk"

  DeleteRegKey HKLM "${UNINST_KEY}"

  ; The library data in %ProgramData%\LANcast is deliberately left in place — an
  ; uninstall must not destroy a user's library. Removing it is a manual choice.
SectionEnd
