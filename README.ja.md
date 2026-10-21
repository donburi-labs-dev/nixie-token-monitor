# Nixie Token Monitor — 裏側プログラム

XENEON EDGE（iCUE）用ウィジェット **Nixie Token Monitor** のための、無料の裏側プログラムです。
ウィジェットは、Claude Code が動いている様子をニキシー管で表示します。

- 今日のトークン数
- イベントのたびに1歩進むデカトロン（輪）
- セッションごとの状態ランプ
- 承認待ちのときに点滅する枠

ウィジェット本体は Elgato Marketplace で配布しています。このリポジトリには、ウィジェットに必要な
小さなプログラム（`ccnixie-hook.exe`）が入っています。

![XENEON EDGE に表示した Nixie Token Monitor](docs/widget.png)

> Anthropic、CORSAIR、Elgato とは関係ありません。「Claude」「Claude Code」は Anthropic の商標です。

## 必要なもの

- Windows 10 / 11
- Claude Code
- iCUE と XENEON EDGE、Nixie Token Monitor ウィジェット

## 導入

1. [最新のリリース](../../releases/latest) から `ccnixie-hook.exe` をダウンロードします。
2. ダウンロードしたフォルダで、PowerShell から実行します。

   ```powershell
   .\ccnixie-hook.exe install
   ```

   ファイルをダブルクリックして `y` と答えても導入できます。
   署名のないプログラムなので、「WindowsによってPCが保護されました」と出ることがあります。
   その場合は「詳細情報 → 実行」を選ぶか、先に `Unblock-File .\ccnixie-hook.exe` を実行してください。
3. Claude Code を起動し直して、1回やりとりします。ウィジェットのデモ表示が、実際の数字に切り替わります。

動作の確認:

```powershell
& "$HOME\.claude\icue\ccnixie-hook.exe" status
```

`8/8 events registered` と表示され、`updated` の時刻が新しければ完了です。

## `install` が行うこと

- 自分自身を `%USERPROFILE%\.claude\icue\ccnixie-hook.exe` にコピーします。
- `%USERPROFILE%\.claude\settings.json` の次の8つのイベントに、フックを1件ずつ追加します。
  `SessionStart`、`UserPromptSubmit`、`PreToolUse`、`PostToolUse`、`Notification`、
  `SubagentStop`、`Stop`、`SessionEnd`
- ほかの設定やフックは、並び順も含めてそのまま残します。
- 書き換える前に、控え（`settings.json.ccnixie-bak-日時`）を作ります。
- `settings.json` がJSONとして壊れている場合は、何行目付近かを表示して止まり、何も変更しません。
- 更新などで `install` をもう一度実行しても、自分の登録を置き換えるだけで、重複はしません。

## 削除

```powershell
& "$HOME\.claude\icue\ccnixie-hook.exe" uninstall
```

自分のフックの登録だけを外します（控えを作ります）。そのあと `%USERPROFILE%\.claude\icue`
フォルダを削除し、iCUE でウィジェットを外してください。

## データの扱い

- すべてPCの中で完結します。**ネットワークには一切接続しません。**
- 読むのは、Claude Code から渡されるイベントと、そのセッションの記録ファイルだけです。
  記録ファイルは**トークン数（usage）を数えるためだけに**読み、プロンプトやコードを保存・複製することはありません。
- 書き出すのは `%USERPROFILE%\.claude\icue\state.json` の1ファイルだけです
  （トークンの合計、セッションのフォルダ名、状態、ツール名）。
- Claude のログイン情報は読まず、APIも呼びません。

## 制限

- Windows のみです（iCUE が Windows 専用のため）。
- サブエージェントのトークン（別の記録ファイル）はまだ数えていません。
- 数値はPC内の記録から数えた表示用のもので、請求額やプランの上限とは一致しません。

## サポート

[Issues](../../issues) からお知らせください。

## AIの利用について

このプロジェクトは Claude（Anthropic）の支援を受けて開発しました。

## ライセンス

[MIT](LICENSE)
