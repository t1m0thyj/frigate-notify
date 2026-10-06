package notifier

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/0x2142/frigate-notify/config"
	"github.com/0x2142/frigate-notify/models"
)

func TestSendTestAlertCooldown(t *testing.T) {
	for _, active := range []bool{false, true} {
		name := "fresh cooldown"
		if active {
			name = "active cooldown"
		}
		t.Run(name, func(t *testing.T) {
			originalConfig := config.ConfigData
			originalLast := config.Internal.Status.LastNotification
			originalWebhookStatus := config.Internal.Status.Notifications.Webhook
			t.Cleanup(func() {
				config.ConfigData = originalConfig
				config.Internal.Status.LastNotification = originalLast
				config.Internal.Status.Notifications.Webhook = originalWebhookStatus
				notificationCooldown = cooldownTracker{}
			})

			requests := make(chan WebhookPayload, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload WebhookPayload
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("invalid webhook payload: %v", err)
				}
				w.WriteHeader(http.StatusOK)
				requests <- payload
			}))
			defer server.Close()

			settings := models.General{Cooldown: 60, CameraCooldown: map[string]int{"doorbell": 120}}
			config.ConfigData = config.Config{Alerts: models.Alerts{
				General: settings,
				Webhook: []models.Webhook{{
					AlertCommon: models.AlertCommon{
						Enabled: true,
						Filters: models.AlertFilter{Cameras: []string{"doorbell"}},
					},
					Server: server.URL,
					Method: "POST",
				}},
			}}
			config.Internal.Status.Notifications.Webhook = make([]models.NotifierStatus, 1)
			notificationCooldown = cooldownTracker{}
			if active {
				notificationCooldown.allow("doorbell", settings)
			}
			lastGlobal := notificationCooldown.lastGlobal
			lastCamera := notificationCooldown.lastCamera["doorbell"]

			// Manual tests still obey provider filters and safely handle an empty response.
			sentinel := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			config.Internal.Status.LastNotification = sentinel
			SendTestAlert(nil)
			SendTestAlert([]models.Event{{Camera: "driveway", ID: "filtered"}})
			if config.Internal.Status.LastNotification != sentinel {
				t.Fatal("empty or provider-filtered tests must not dispatch")
			}

			SendTestAlert([]models.Event{{Camera: "doorbell", ID: "manual-test"}})
			select {
			case payload := <-requests:
				if payload.ID != "manual-test" {
					t.Fatalf("unexpected dispatched event: %s", payload.ID)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("manual test was not delivered")
			}
			if notificationCooldown.lastGlobal != lastGlobal || notificationCooldown.lastCamera["doorbell"] != lastCamera {
				t.Fatal("manual tests must not start or extend either cooldown")
			}
			if got := notificationCooldown.allow("doorbell", settings); got == active {
				t.Fatalf("next real alert allowed=%v, want %v", got, !active)
			}
		})
	}
}
