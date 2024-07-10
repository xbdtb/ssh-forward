# go tool dist list
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -tags local -o ./bin/ssh-forward_darwin_amd64 main.go
upx ./bin/ssh-forward_darwin_amd64