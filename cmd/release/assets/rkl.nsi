; Per-user NSIS installer for the Go build of Rust King Ledger (`task package:windows`).
;
; Installs to %LOCALAPPDATA%\Rust King Ledger without UAC, under the same uninstall and
; product registry keys as the Tauri installer, so it upgrades a Tauri install in place.
;
; cmd/release passes these with -D:
;   VERSION    app version, e.g. 0.1.0-rc.1
;   VERSION4   numeric version, e.g. 0.1.0.0
;   BINARY     path of rkl.exe
;   ICON       path of icon.ico
;   LICENSE    path of LICENSE
;   WEBVIEW2   path of MicrosoftEdgeWebview2Setup.exe (the evergreen bootstrapper)
;   OUTFILE    installer path to write
Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma

!define PRODUCTNAME "Rust King Ledger"
!define MANUFACTURER "RKL contributors"
!define MAINBINARYNAME "rkl"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${PRODUCTNAME}"
!define MANUPRODUCTKEY "Software\${MANUFACTURER}\${PRODUCTNAME}"
!define RUNKEY "Software\Microsoft\Windows\CurrentVersion\Run"
!define WEBVIEW2APPGUID "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!include "WinVer.nsh"
!include "FileFunc.nsh"

Name "${PRODUCTNAME}"
OutFile "${OUTFILE}"
RequestExecutionLevel user
InstallDir "$LOCALAPPDATA\${PRODUCTNAME}"
InstallDirRegKey HKCU "${MANUPRODUCTKEY}" ""
ShowInstDetails show

VIProductVersion "${VERSION4}"
VIFileVersion "${VERSION4}"
VIAddVersionKey "ProductName" "${PRODUCTNAME}"
VIAddVersionKey "CompanyName" "${MANUFACTURER}"
VIAddVersionKey "FileDescription" "${PRODUCTNAME} installer"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "Copyright (c) 2026 RKL contributors. Apache-2.0."

!define MUI_ICON "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${MAINBINARYNAME}.exe"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${LICENSE}"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Function .onInit
  ${IfNot} ${AtLeastWin10}
  ${OrIfNot} ${RunningX64}
    MessageBox MB_OK|MB_ICONSTOP "${PRODUCTNAME} needs 64-bit Windows 10 or later."
    SetErrorLevel 64
    Quit
  ${EndIf}
  SetShellVarContext current
  SetRegView 64
FunctionEnd

Function un.onInit
  SetShellVarContext current
  SetRegView 64
FunctionEnd

; Ask a running RKL to close (it has no console, so taskkill without /F posts WM_CLOSE to its
; windows), then force it, so the exe can be replaced.
!macro StopRunningApp
  ReadEnvStr $1 USERNAME
  nsExec::Exec '"$SYSDIR\taskkill.exe" /IM ${MAINBINARYNAME}.exe /FI "USERNAME eq $1"'
  Pop $0
  Sleep 500
  nsExec::Exec '"$SYSDIR\taskkill.exe" /F /IM ${MAINBINARYNAME}.exe /FI "USERNAME eq $1"'
  Pop $0
  Sleep 500
!macroend

Section "WebView2"
  ; Skip when the evergreen runtime is installed machine-wide or for this user.
  ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\${WEBVIEW2APPGUID}" "pv"
  ${If} $0 == ""
  ${OrIf} $0 == "0.0.0.0"
    ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\${WEBVIEW2APPGUID}" "pv"
  ${EndIf}
  ${If} $0 == ""
  ${OrIf} $0 == "0.0.0.0"
    DetailPrint "Installing the WebView2 runtime"
    InitPluginsDir
    File "/oname=$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" "${WEBVIEW2}"
    ExecWait '"$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install' $1
    ${If} $1 <> 0
      DetailPrint "WebView2 setup exited with $1"
    ${EndIf}
  ${EndIf}
SectionEnd

Section "Install"
  !insertmacro StopRunningApp
  SetOutPath "$INSTDIR"
  File "/oname=${MAINBINARYNAME}.exe" "${BINARY}"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateShortCut "$SMPROGRAMS\${PRODUCTNAME}.lnk" "$INSTDIR\${MAINBINARYNAME}.exe"

  WriteRegStr HKCU "${MANUPRODUCTKEY}" "" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "${PRODUCTNAME}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$\"$INSTDIR\${MAINBINARYNAME}.exe$\""
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "${MANUFACTURER}"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$\"$INSTDIR$\""
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
  WriteRegStr HKCU "${UNINSTKEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINSTKEY}" "EstimatedSize" "$0"
SectionEnd

Section "Uninstall"
  !insertmacro StopRunningApp
  Delete "$INSTDIR\${MAINBINARYNAME}.exe"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\${PRODUCTNAME}.lnk"
  Delete "$DESKTOP\${PRODUCTNAME}.lnk"
  ; The autostart entry RKL writes when "start at login" is on.
  DeleteRegValue HKCU "${RUNKEY}" "${MAINBINARYNAME}"
  DeleteRegKey HKCU "${UNINSTKEY}"
  DeleteRegKey HKCU "${MANUPRODUCTKEY}"
  DeleteRegKey /ifempty HKCU "Software\${MANUFACTURER}"
SectionEnd
