# versioninfo.json

This file is used by [goversioninfo](https://github.com/josephspurrier/goversioninfo) to generate the
Microsoft Windows File Properties/Version Info.

`versioninfo.json` is a template for the Windows File Properties/Version Info.
No build step currently fills it in or runs goversioninfo, so the released
`proxiport.exe` carries no version resource. The release pipeline does not
build an MSI today either; the template is kept for whoever next cuts one.

To embed a version resource, fill in the version fields in
`opt/resource/versioninfo.json` by hand, then generate `resource.syso` in
`cmd/proxiport` before running `go build`:

```shell
cd ./cmd/proxiport
goversioninfo.exe -icon=../../opt/resource/images/app.ico ../../opt/resource/versioninfo.json
```

goversioninfo reads `versioninfo.json` from the current directory when you
give it no input file, and `cmd/proxiport` holds its own, separate
`versioninfo.json`, so pass this template's path explicitly as above.

This creates the file `resource.syso`, and `go build` (or `GOOS=windows go build`)
picks it up to embed the data into the .exe file.

If `goversioninfo` is not installed on your system, install with:
```powershell
go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
$ENV:PATH="$ENV:PATH;$(go env GOPATH)\bin"
```
