# Contributing

## ビルド

Goがインストールされた環境で、以下のようにクロスコンパイルできます。
標準ライブラリのみで構成されているので、`go build` だけで完結します。

```bash
GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o dist/claude-scheduler-linux-amd64 .
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/claude-scheduler-windows-amd64.exe .
GOOS=darwin  GOARCH=arm64 go build -ldflags="-s -w" -o dist/claude-scheduler-darwin-arm64 .
```

## テスト

```bash
go test ./...
```

`crontab` / `schtasks` の呼び出しは差し替えているので、どのOSでも全テストが動きます。
ブランチへのpushごとに、GitHub Actions (`.github/workflows/ci.yml`) がLinux / Windows / macOSでテストを実行します。

## リリース

`v1.2.3` のようなセマンティックバージョンのタグをpushすると、GitHub Actions (`.github/workflows/cd.yml`) が
GoReleaserで3種類のバイナリをビルドし、Releasesページに公開します。`v1.2.3-rc.1` のようなタグはプレリリースになります。

```bash
git tag v1.0.0
git push origin v1.0.0
```
