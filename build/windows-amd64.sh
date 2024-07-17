# go tool dist list
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags local -ldflags="-s -w" -o ./bin/ssh-forward_windows_amd64.exe cmd/sshf/main.go
upx ./bin/ssh-forward_windows_amd64.exe