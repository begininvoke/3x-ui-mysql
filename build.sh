#!/usr/bin/bash
archs=(amd64 arm64)
os=(linux darwin)

for o in ${os[@]}
do
for arch in ${archs[@]}
do
        env CGO_ENABLED=0 GOOS=${o} GOARCH=${arch} go build -ldflags "-w -s" -o bin/x-ui_${o}_${arch}
done
done
