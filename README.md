# pubsub

A generic pubsub implementation for Go.

## pubsub over ssh

The simplest pubsub system for everyday automation needs.

Using `wish` we can integrate our pubsub system into an SSH app.

[![asciicast](https://asciinema.org/a/674287.svg)](https://asciinema.org/a/674287)

```bash
# term 1
mkdir ./ssh_data
cat ~/.ssh/id_ed25519 ./ssh_data/authorized_keys
go run ./cmd/example

# term 2
ssh -p 2222 localhost sub xyz

# term 3
echo "hello world" | ssh -p 2222 localhost pub xyz
```
