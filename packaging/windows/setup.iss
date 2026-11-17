; PC Inventory Manager - Windows installer
; Build:  ISCC.exe /DAppVersion=1.0.0 packaging\windows\setup.iss
;         (or: powershell -File scripts\build-windows.ps1)
; Expects the release binaries in ..\..\dist

#define AppName      "PC Inventory Manager"
#define AppPublisher "PC Inventory Manager"
#ifndef AppVersion
  #define AppVersion "1.0.0"
#endif
#define SourceDir    "..\..\dist"

[Setup]
AppId={{8B4F2D31-7C1E-4A52-9B77-2E4F6A1D9C10}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher={#AppPublisher}
VersionInfoVersion={#AppVersion}
DefaultDirName={autopf}\PC Inventory Manager
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
OutputDir=..\..\dist
OutputBaseFilename=pcim-{#AppVersion}-windows-setup
UninstallDisplayIcon={app}\pcim-server.exe
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
MinVersion=10.0
ChangesAssociations=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Types]
Name: "full";   Description: "Server and agent (recommended)"
Name: "server"; Description: "Server only"
Name: "agent";  Description: "Agent only"
Name: "custom"; Description: "Custom"; Flags: iscustom

[Components]
Name: "server"; Description: "PC Inventory Manager server (dashboard, API, database)"; Types: full server custom
Name: "agent";  Description: "Agent for this computer (reports hardware and performance)"; Types: full agent custom

[Tasks]
Name: "server_service"; Description: "Run the server as a Windows service (starts at boot)"; Components: server
Name: "firewall";       Description: "Open TCP {code:GetPort} in Windows Firewall (private networks only)"; Components: server; Flags: checkedonce
Name: "desktopicon";    Description: "Create a dashboard shortcut on the desktop"; Components: server; Flags: unchecked
Name: "enroll";         Description: "Enroll this computer with a server now"; Components: agent; Flags: checkedonce

[Files]
Source: "{#SourceDir}\pcim-server.exe"; DestDir: "{app}"; Components: server; Flags: ignoreversion
Source: "{#SourceDir}\pcim-agent.exe";  DestDir: "{app}"; Components: agent;  Flags: ignoreversion
Source: "..\..\README.md";              DestDir: "{app}"; Components: server agent; Flags: ignoreversion isreadme
Source: "..\..\docs\*.md";              DestDir: "{app}\docs"; Components: server agent; Flags: ignoreversion
Source: "server.env.sample";            DestDir: "{app}"; Components: server; Flags: ignoreversion

[Dirs]
Name: "{commonappdata}\PCInventoryManager\Server"; Permissions: users-modify; Components: server
Name: "{commonappdata}\PCInventoryManager\Agent";  Permissions: users-modify; Components: agent

[Icons]
Name: "{group}\PC Inventory Manager Dashboard"; Filename: "http://localhost:{code:GetPort}"; Components: server
Name: "{group}\PC Inventory Manager Data Folder"; Filename: "{commonappdata}\PCInventoryManager"; Components: server
Name: "{group}\Uninstall PC Inventory Manager"; Filename: "{uninstallexe}"
Name: "{autodesktop}\PC Inventory Manager"; Filename: "http://localhost:{code:GetPort}"; Components: server; Tasks: desktopicon

[Run]
Filename: "{app}\pcim-server.exe"; Parameters: "service install --env ""{commonappdata}\PCInventoryManager\Server\server.env"""; StatusMsg: "Installing the server service..."; Flags: runhidden waituntilterminated; Components: server; Tasks: server_service
Filename: "{app}\pcim-server.exe"; Parameters: "service start"; StatusMsg: "Starting the server..."; Flags: runhidden waituntilterminated; Components: server; Tasks: server_service
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall add rule name=""PC Inventory Manager (TCP {code:GetPort})"" dir=in action=allow protocol=TCP localport={code:GetPort} profile=private"; Flags: runhidden; Components: server; Tasks: firewall
Filename: "{app}\pcim-agent.exe"; Parameters: "service install"; StatusMsg: "Installing the agent service..."; Flags: runhidden waituntilterminated; Components: agent; Tasks: enroll
Filename: "http://localhost:{code:GetPort}"; Description: "Open the dashboard"; Flags: shellexec nowait postinstall skipifsilent; Components: server

[UninstallRun]
Filename: "{app}\pcim-server.exe"; Parameters: "service stop";   Flags: runhidden waituntilterminated; RunOnceId: "StopServerSvc"; Components: server
Filename: "{app}\pcim-server.exe"; Parameters: "service uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "DelServerSvc"; Components: server
Filename: "{app}\pcim-agent.exe";  Parameters: "service stop";   Flags: runhidden waituntilterminated; RunOnceId: "StopAgentSvc";  Components: agent
Filename: "{app}\pcim-agent.exe";  Parameters: "service uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "DelAgentSvc"; Components: agent
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall delete rule name=""PC Inventory Manager (TCP {code:GetPort})"""; Flags: runhidden; RunOnceId: "DelFirewallRule"; Components: server

[UninstallDelete]
Type: filesandordirs; Name: "{app}\docs"

[Code]
var
  PortPage: TInputQueryWizardPage;
  AgentPage: TInputQueryWizardPage;
  Port: String;

function GetPort(Param: String): String;
begin
  if Port = '' then
    Result := '8080'
  else
    Result := Port;
end;

procedure InitializeWizard;
begin
  PortPage := CreateInputQueryPage(wpSelectTasks,
    'Server settings', 'Which port should the server use?',
    'Agents connect to the server on this port.');
  PortPage.Add('Port:', False);
  PortPage.Values[0] := '8080';

  AgentPage := CreateInputQueryPage(PortPage.ID,
    'Agent settings', 'Where is the inventory server?',
    'Enter the address of the computer running PC Inventory Manager, for example' + #13#10 +
    'http://192.168.1.100:8080. Approval in the dashboard is still required.');
  AgentPage.Add('Server address:', False);
  AgentPage.Values[0] := 'http://127.0.0.1:8080';
end;

function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := False;
  if PageID = AgentPage.ID then
    Result := not WizardIsComponentSelected('agent');
end;

function NextButtonClick(CurPageID: Integer): Boolean;
var
  P: Integer;
begin
  Result := True;
  if CurPageID = PortPage.ID then
  begin
    P := StrToIntDef(PortPage.Values[0], 0);
    if (P < 1) or (P > 65535) then
    begin
      MsgBox('Enter a port between 1 and 65535.', mbError, MB_OK);
      Result := False;
      Exit;
    end;
    Port := PortPage.Values[0];
    AgentPage.Values[0] := 'http://127.0.0.1:' + Port;
  end;
end;

function EnrollAgent(Address: String): Boolean;
var
  ResultCode: Integer;
  Cfg: String;
begin
  Cfg := ExpandConstant('{commonappdata}\PCInventoryManager\Agent\agent.json');
  Exec(ExpandConstant('{app}\pcim-agent.exe'), 'config --config "' + Cfg + '" --server "' + Address + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Result := Exec(ExpandConstant('{app}\pcim-agent.exe'), 'enroll --config "' + Cfg + '" --server "' + Address + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) and (ResultCode = 0);
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  EnvPath: String;
  Address: String;
begin
  if CurStep = ssPostInstall then
  begin
    if WizardIsComponentSelected('server') then
    begin
      EnvPath := ExpandConstant('{commonappdata}\PCInventoryManager\Server\server.env');
      SaveStringToFile(EnvPath,
        '# PC Inventory Manager server configuration.' + #13#10 +
        '# 127.0.0.1 = this computer only; 0.0.0.0 = accept agents on the LAN.' + #13#10 +
        'PCIM_LISTEN_HOST=0.0.0.0' + #13#10 +
        'PCIM_PORT=' + GetPort('') + #13#10 +
        'PCIM_OFFLINE_THRESHOLD_SEC=90' + #13#10, False);
    end;

    if WizardIsComponentSelected('agent') and WizardIsTaskSelected('enroll') then
    begin
      Address := Trim(AgentPage.Values[0]);
      if Address <> '' then
      begin
        if not EnrollAgent(Address) then
          MsgBox('The agent could not reach ' + Address + '.' + #13#10 +
                 'Installation is complete; approve and enroll this computer later with:' + #13#10 +
                 '  pcim-agent.exe enroll --server ' + Address, mbInformation, MB_OK);
      end;
    end;
  end;
end;
