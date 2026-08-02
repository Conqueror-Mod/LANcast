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

; Branding: the app icon for the installer, the wordmark on the welcome/finish
; pages.
!define MUI_ICON "..\assets\lancast.ico"
!define MUI_UNICON "..\assets\lancast.ico"
!define MUI_WELCOMEFINISHPAGE_BITMAP "welcome.bmp"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "welcome.bmp"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "..\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\lancast.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open LANcast"
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\LANcast"

Section "LANcast"
  SetOutPath "$INSTDIR"
  File "lancastd.exe"
  File "lancast.exe"
  File "..\README.md"
  File "..\LICENSE"

  ; Register the server as a service pinned to the machine-wide data dir, and
  ; start it. `service install` refuses an unset --data, so the pin is enforced.
  nsExec::ExecToLog '"$INSTDIR\lancastd.exe" service install'
  nsExec::ExecToLog '"$INSTDIR\lancastd.exe" service start'

  ; Shortcuts point at the launcher, not the daemon.
  CreateDirectory "$SMPROGRAMS\LANcast"
  CreateShortcut "$SMPROGRAMS\LANcast\LANcast.lnk" "$INSTDIR\lancast.exe"
  CreateShortcut "$DESKTOP\LANcast.lnk" "$INSTDIR\lancast.exe"

  WriteUninstaller "$INSTDIR\uninstall.exe"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "LANcast"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\lancast.exe"
  WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "LANcast"
SectionEnd

Section "Uninstall"
  ; Stop and remove the service before deleting its binary.
  nsExec::ExecToLog '"$INSTDIR\lancastd.exe" service stop'
  nsExec::ExecToLog '"$INSTDIR\lancastd.exe" service uninstall'

  Delete "$INSTDIR\lancastd.exe"
  Delete "$INSTDIR\lancast.exe"
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
