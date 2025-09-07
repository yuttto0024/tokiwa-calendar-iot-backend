# tokiwa-calendar-iot-backend

`tokiwa-calendar`プロジェクトにおける、ハードウェア連携を担うサーバーレス・バックエンド部分。
指定された時間にバックエンド処理を自動実行し、MQTTブローカーを介してメッセージを送信する。


## 目的

このバックエンドは、ユーザーがカレンダーに登録した予定の時刻をトリガーに、自動で処理を実行することを目的とする。
ユーザーからの直接のリクエストを処理する`tokiwa-calendar-backend`とは異なり、こちらは**時間やイベントをきっかけに自律的に動作する**という役割を担う。
これにより、リマインド機能や期日通知など、ハードウェアと連携した機能を実現する。


## 実装した機能

  * **スケジュール実行機能**: ユーザーがFirestoreに登録したタスクの`deadline`（期限）を監視し、指定時刻になったタスクを自動で実行する。


## 技術スタック

この機能は、Google Cloudのサービスを組み合わせて構築されている。

  * **データベース**: Firebase Firestore
  * **バックエンドロジック**: Google Cloud Functions (Go)
      * Firestoreを定期的にチェックし、期限が来たタスクを処理する本体。
  * **トリガー**: **Google Cloud Scheduler**
      * 1分毎にCloud Functionsを呼び出すタイマーを担う。
  * **メッセージング**: **EMQX (MQTT Broker)**
      * 処理結果（通知など）を送信するためのメッセージブローカー。

### 処理の流れ

1.  **【トリガー】** Cloud Schedulerが1分毎にCloud FunctionのURLをHTTPリクエストで呼び出す。
2.  **【タスク検索】** 起動したCloud Functionは、Firestoreの`scheduled_tasks`コレクションに「ステータスが`pending`で、かつ期限(`deadline`)が現在時刻以前のタスク」をfindする。
3.  **【タスク実行】** 条件に一致したタスクが見つかると、そのタスクの`mqttMessage`を`mqttTopic`宛にMQTTブローカーへPublishする。
4.  **【状態更新】** 処理が完了したタスクは、`status`を`processed`に更新し、二重実行を防ぐ。

-----

## Firestoreのデータ構造

このバックエンドは、`scheduled_tasks`という名前のコレクションを参照する。

### `scheduled_tasks`コレクション

**目的**: バックエンドが期限をチェックするためだけの、新たに追加するコレクション。

**JSON構造の例:**

```json
{
  "userId": "OUIEBZId",
  "deadline": "September 7, 2025 at 10:00:00 AM UTC+9",        // Firestore Timestamp型
  "status": "pending",                                         // "pending", "processed", "error"など
  "mqttTopic": "calendar/reminders/OUIEBZId",                  // 送信先のMQTTトピック
  "mqttMessage": "{\"title\":\"定例会議\",\"time\":\"10:00\"}", // 送信するメッセージ(JSON文字列)
  "createdAt": "September 6, 2025 at 11:17:12 AM UTC+9"        // Firestore Timestamp型
}
```

**重要な注意点**:
フロントエンドでユーザーの予定（例: `schedules_prod`コレクション）を保存・更新する際には、**同時に**この`scheduled_tasks`コレクションにも対応するタスクドキュメントを作成・更新・削除する\*\*デュアルライト（二重書き込み）\*\*処理が必要。

### 必要なインデックス

このバックエンドがFirestoreを効率的にクエリするために、以下の複合インデックスが必要。

  * **コレクションID**: `scheduled_tasks`
  * **フィールド**:
    1.  `status` (昇順)
    2.  `deadline` (昇順)

-----

## 環境変数

このCloud Functionは、以下の環境変数で設定する。

  * `GCP_PROJECT`: Google CloudのプロジェクトID
  * `MQTT_BROKER`: MQTTブローカーの接続アドレス (例: `mqtts://...:8883`)
  * `MQTT_USER`: MQTTブローカーのユーザー名
  * `MQTT_PASS`: MQTTブローカーのパスワード

## デプロイ方法

1.  Google Cloud CLIで認証とプロジェクト設定を済ませる (`gcloud auth login`, `gcloud config set project`)。
2.  リポジトリのルートで、以下のコマンドを実行。

```bash
gcloud functions deploy CheckTasksAndPublish \
--gen2 \
--runtime go122 \
--region asia-northeast1 \
--source . \
--entry-point CheckTasksAndPublish \
--trigger-http \
--allow-unauthenticated \
--set-env-vars MQTT_BROKER="...",MQTT_USER="...",MQTT_PASS="...",GCP_PROJECT="..."
```
