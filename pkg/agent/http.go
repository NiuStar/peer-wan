package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
)

func postJSON(client *http.Client, url, authToken, provisionToken string, payload interface{}) error {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	setAuth(req, authToken, provisionToken)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func setAuth(req *http.Request, authToken, provisionToken string) {
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	if provisionToken != "" {
		req.Header.Set("X-Provision-Token", provisionToken)
	}
}
