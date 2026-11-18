Set WshShell = CreateObject("WScript.Shell")
strPath = WScript.ScriptFullName
Set objFSO = CreateObject("Scripting.FileSystemObject")
Set objFile = objFSO.GetFile(strPath)
strFolder = objFSO.GetParentFolderName(objFile)
WshShell.CurrentDirectory = strFolder
WshShell.Run """" & strFolder & "\bell-budut.exe"" -port 8080", 0, False

