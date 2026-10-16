# versioninfo.json

This file is used by [goversioninfo](https://github.com/josephspurrier/goversioninfo) to generate the
Microsoft Windows File Properties/Version Info.

`versioninfo.json` is a template. Values are inserted before compiling the Windows version of the proxiport client.
The release pipeline does not build an MSI today; the template is kept
for whoever next cuts one.

Example:
```shell
cd ./cmd/proxiport
goversioninfo.exe
```

This creates the file `resource.syso` and `go build` will pick it up to embed the data into the .exe file.

If `goversioninfo` is not installed on your system, install with:
```powershell
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
$ENV:PATH="$ENV:PATH;$($env:home)\go\bin"
```