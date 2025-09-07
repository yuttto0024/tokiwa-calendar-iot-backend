package functions

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"google.golang.org/api/iterator"
)

// 環境変数から読み込む情報
var (
	projectID  = os.Getenv("GCP_PROJECT")
	mqttBroker = os.Getenv("MQTT_BROKER")
	mqttUser   = os.Getenv("MQTT_USER")
	mqttPass   = os.Getenv("MQTT_PASS")
	clientID   = "gcp-function-client"
)

// Task Firestoreのドキュメント構造
type Task struct {
	Deadline time.Time `firestore:"deadline"`
	Message  string    `firestore:"mqttMessage"` // フィールド名をFirestoreに合わせる
	Topic    string    `firestore:"mqttTopic"`   // フィールド名をFirestoreに合わせる
	Status   string    `firestore:"status"`
}

func init() {
	// HTTPトリガーでこの関数を登録
	functions.HTTP("CheckTasksAndPublish", CheckTasksAndPublish)
}

// CheckTasksAndPublish Cloud Schedulerから呼び出されるメイン関数
func CheckTasksAndPublish(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		// log.Fatalfは関数を停止させてしまうので、エラーを記録してHTTPエラーを返す形に変更
		log.Printf("ERROR: Failed to create Firestore client: %v", err)
		http.Error(w, "Failed to create Firestore client", http.StatusInternalServerError)
		return
	}
	defer client.Close()

	now := time.Now()
	tasksRef := client.Collection("scheduled_tasks")
	// "status"が"pending"で、"deadline"が現在時刻以前のタスクを検索
	query := tasksRef.Where("status", "==", "pending").Where("deadline", "<=", now)
	iter := query.Documents(ctx)

	mqttClient := createMqttClient()
	// mqttClientがnilでない場合のみ切断処理を遅延実行
	if mqttClient != nil {
		defer mqttClient.Disconnect(250)
	}

	processedCount := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("ERROR: Failed to iterate tasks: %v", err)
			http.Error(w, "Failed to iterate tasks", http.StatusInternalServerError)
			return
		}

		var task Task
		doc.DataTo(&task)

		log.Printf("INFO: Processing task %s: %s", doc.Ref.ID, task.Message)

		// MQTTにPublish
		publishMessage(mqttClient, task.Topic, task.Message)

		// タスクのステータスを"processed"に更新して再実行を防ぐ
		_, err = doc.Ref.Update(ctx, []firestore.Update{
			{Path: "status", Value: "processed"},
		})
		if err != nil {
			log.Printf("ERROR: Failed to update task status for %s: %v", doc.Ref.ID, err)
		} else {
			processedCount++
		}
	}

	fmt.Fprintf(w, "Task check completed. %d tasks processed.", processedCount)
}

// --- MQTT関連の関数 ---

func createMqttClient() mqtt.Client {
	opts := mqtt.NewClientOptions().AddBroker(mqttBroker).SetUsername(mqttUser).SetPassword(mqttPass).SetClientID(clientID)
	opts.SetConnectTimeout(10 * time.Second)
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		// 【修正点①】panicをなくし、エラーログを出力してnilを返す
		log.Printf("ERROR: Failed to connect to MQTT broker: %v", token.Error())
		return nil
	}
	return client
}
func publishMessage(client mqtt.Client, topic string, payload string) {
	if client == nil || !client.IsConnected() {
		log.Println("WARN: MQTT client is not connected. Skipping publish.")
		return
	}
	token := client.Publish(topic, 1, false, payload)
	token.Wait()
	log.Printf("INFO: Published to %s: %s", topic, payload)
}