# claude-scheduler

Claude Codeの5時間利用枠(rolling window)を狙ったタイミングで開始させるための
軽量ping登録ツールです。Node.jsなどのランタイムは不要で、OSごとの単一実行ファイル
として配布・実行できます。

## 前提条件

- 実行環境に `claude` コマンド (Claude Code CLI) がPATHに通っていること
- Linux/macOS: `cron` (`crontab`コマンド) が使えること
- Windows: `schtasks` (標準搭載)

## 配布ファイル

| ファイル | 対象 |
|---|---|
| `claude-scheduler-linux-amd64` | Linux (x86_64) |
| `claude-scheduler-windows-amd64.exe` | Windows (x86_64) |
| `claude-scheduler-darwin-arm64` | macOS (Apple Silicon) |

## 使い方

### Linux / macOS

```bash
chmod +x claude-scheduler-linux-amd64
sudo mv claude-scheduler-linux-amd64 /usr/local/bin/claude-scheduler   # PATHに置くと楽

claude-scheduler add 07:00 12:00 17:00
claude-scheduler list
claude-scheduler remove 12:00     # 指定した時刻のジョブを削除
claude-scheduler remove all      # すべて削除
```

### Windows (PowerShell)

```powershell
# 適当なフォルダに置いてPATHを通す、もしくはフルパスで実行
.\claude-scheduler-windows-amd64.exe add 07:00 12:00 17:00
.\claude-scheduler-windows-amd64.exe list
.\claude-scheduler-windows-amd64.exe remove 12:00
.\claude-scheduler-windows-amd64.exe remove all
```

### 共通オプション

```
claude-scheduler add <時刻...> [オプション]

--days <list>    カンマ区切りの曜日 (sun,mon,tue,wed,thu,fri,sat)
                 デフォルト: mon,tue,wed,thu,fri
--prompt <text>  送信するプロンプト文字列。デフォルト: "ok"
--log <path>     実行結果を追記するログファイルのパス
```

- `add` は同じ時刻のジョブだけを上書きし、それ以外の時刻のジョブは残します。すべて消すには `remove all` を使います。
- `remove <時刻...>` は指定した時刻のジョブを、`remove all` はすべてのジョブを削除します。どちらもこのツールが登録したジョブだけが対象で、それ以外のcron行やタスクには影響しません。
- `--prompt` / `--log` には `"` と改行は使えません。

例:
```bash
claude-scheduler add 07:00 12:00 17:00 --prompt "hello" --log ~/claude-ping.log
```

## ソースからビルドする場合

`main.go` を使い、Goがインストールされた環境で以下のようにクロスコンパイルできます。

```bash
GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o claude-scheduler-linux-amd64 .
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o claude-scheduler-windows-amd64.exe .
GOOS=darwin  GOARCH=arm64 go build -ldflags="-s -w" -o claude-scheduler-darwin-arm64 .
```

標準ライブラリのみので構成されているので、`go build` だけで完結します。

テストは `go test ./...` で実行できます。`crontab` / `schtasks` の呼び出しは差し替えているので、どのOSでも全テストが動きます。

## 注意事項

- 各pingは実際にClaude Codeを1回呼び出すため、わずかに利用量を消費します。
- 利用枠の起算タイミングやリセット周期の正確な仕様は変更される可能性があります。
  最新の挙動は公式ドキュメントで確認してください。
  https://docs.claude.com/en/docs/claude-code/overview
- 本ツールはあくまで「早い時間に軽い呼び出しを送って枠を開始させる」という
  スケジューリングの補助であり、利用制限の仕組み自体を変更・回避するものではありません。
