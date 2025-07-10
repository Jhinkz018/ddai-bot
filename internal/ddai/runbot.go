package ddai

import (
	"ddai-bot/internal/captcha"
	"ddai-bot/internal/utils"
	"encoding/json"
	"fmt"
	"time"
)

type ddaiRunBot struct {
	proxy      string
	email      string
	password   string
	currentNum int
	total      int
	captcha    *captcha.CaptchaServices
	httpClient *HTTPClient
}

func NewDdaiRunBot(email, password, proxy string, currentNum, total int) *ddaiRunBot {
	return &ddaiRunBot{
		proxy:      proxy,
		email:      email,
		password:   password,
		currentNum: currentNum,
		total:      total,
		captcha:    captcha.NewCaptchaServices(),
		httpClient: NewHTTPClient(proxy, currentNum, total),
	}
}

func (m *ddaiRunBot) SingleProses() error {
	for attempt := 1; attempt <= retryCount; attempt++ {
		utils.LogMessage(m.currentNum, m.total, fmt.Sprintf("Attempt %d/%d", attempt, retryCount), "process")

		token, err := m.captcha.SolveCaptcha(m.currentNum, m.total)
		if err != nil {
			utils.LogMessage(m.currentNum, m.total, fmt.Sprintf("Failed to solve captcha: %v", err), "error")
			continue
		}

		accessToken, err := m.loginAccount(m.email, m.password, token)
		if err != nil {
			utils.LogMessage(m.currentNum, m.total, fmt.Sprintf("%v", err), "warning")
			time.Sleep(retryDelay)
			continue
		}

		if err := m.modelResponse(accessToken); err != nil {
			utils.LogMessage(m.currentNum, m.total, fmt.Sprintf("Model response failed: %v", err), "warning")
			time.Sleep(retryDelay)
			continue
		}

		if err := m.onchainTrigger(accessToken); err != nil {
			utils.LogMessage(m.currentNum, m.total, fmt.Sprintf("Onchain trigger failed: %v", err), "warning")
			time.Sleep(retryDelay)
			continue
		}

		utils.LogMessage(m.currentNum, m.total, "Successfully ran auto bot flow", "success")
		return nil
	}

	return fmt.Errorf("failed after %d attempts", retryCount)
}

func (m *ddaiRunBot) loginAccount(username string, password string, captcha string) (string, error) {
	utils.LogMessage(m.currentNum, m.total, fmt.Sprintf("Logging in with username: %s", username), "process")
	payload := map[string]string{
		"username":     username,
		"password":     password,
		"captchaToken": captcha,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %v", err)
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	body, err := m.httpClient.MakeRequestWithBody("POST", "https://auth.ddai.space/login", jsonData, headers)
	if err != nil {
		return "", fmt.Errorf("login request failed: %v", err)
	}

	var response LoginResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to decode response: %v (body: %s)", err, string(body))
	}

	if response.Status == "success" {
		utils.LogMessage(m.currentNum, m.total, "Successfully logged in", "success")
		return response.Data.AccessToken, nil
	}

	errorMsg := response.Error["message"]
	if errorMsg == nil {
		errorMsg = response.Error
	}
	return "", fmt.Errorf("login failed: %v", errorMsg)
}

func (m *ddaiRunBot) getUserTask(accessToken string) ([]map[string]string, error) {
	utils.LogMessage(m.currentNum, m.total, "Fetching user tasks...", "process")

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + accessToken,
	}

	body, err := m.httpClient.MakeRequestWithBody("GET", "https://auth.ddai.space/missions", nil, headers)
	if err != nil {
		return nil, fmt.Errorf("get tasks request failed: %v", err)
	}

	var response MissionsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v (body: %s)", err, string(body))
	}

	utils.LogMessage(m.currentNum, m.total, fmt.Sprintf("Found %d missions", len(response.Data.Missions)), "info")

	var tasks []map[string]string
	for _, task := range response.Data.Missions {
		if task.Status == "PENDING" || task.Status == "idle" || task.Status == "pending" {
			if containsIgnoreCase(task.Title, "invite") {
				continue
			}

			taskInfo := map[string]string{
				"id":   task.ID,
				"name": task.Title,
			}
			tasks = append(tasks, taskInfo)
		}
	}
	return tasks, nil
}

func (m *ddaiRunBot) claimTask(accessToken string, task map[string]string) error {
	utils.LogMessage(m.currentNum, m.total, fmt.Sprintf("Claiming task: %s (ID: %s)", task["name"], task["id"]), "process")

	url := fmt.Sprintf("https://auth.ddai.space/missions/claim/%s", task["id"])
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + accessToken,
	}

	body, err := m.httpClient.MakeRequestWithBody("POST", url, nil, headers)
	if err != nil {
		return fmt.Errorf("claim task request failed: %v", err)
	}

	var result ClaimResponse
	if err := json.Unmarshal(body, &result); err != nil {
		utils.LogMessage(m.currentNum, m.total, fmt.Sprintf("Raw response: %s", string(body)), "warning")
		return fmt.Errorf("failed to decode response: %v", err)
	}

	if result.Status != "success" {
		errorMsg := "unknown error"
		if result.Error != nil {
			if msg, ok := result.Error["message"]; ok {
				errorMsg = fmt.Sprintf("%v", msg)
			}
		}
		utils.LogMessage(m.currentNum, m.total, fmt.Sprintf("Claim failed: %s", errorMsg), "warning")
		return fmt.Errorf("claim task failed: %s", errorMsg)
	}

	utils.LogMessage(m.currentNum, m.total, fmt.Sprintf("Successfully claimed task: %s with rewards: %d requests", task["name"], result.Data.Rewards.Requests), "success")
	return nil
}

type genericResponse struct {
	Status string                 `json:"status"`
	Error  map[string]interface{} `json:"error"`
}

func (m *ddaiRunBot) modelResponse(accessToken string) error {
	headers := map[string]string{
		"Authorization": "Bearer " + accessToken,
	}

	body, err := m.httpClient.MakeRequestWithBody("GET", "https://auth.ddai.space/modelResponse", nil, headers)
	if err != nil {
		return fmt.Errorf("modelResponse request failed: %v", err)
	}

	var resp genericResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("failed to decode response: %v (body: %s)", err, string(body))
	}

	if resp.Status != "success" && resp.Status != "ok" {
		msg := "unknown error"
		if resp.Error != nil {
			if m, ok := resp.Error["message"]; ok {
				msg = fmt.Sprintf("%v", m)
			}
		}
		return fmt.Errorf("modelResponse failed: %s", msg)
	}

	return nil
}

func (m *ddaiRunBot) onchainTrigger(accessToken string) error {
	headers := map[string]string{
		"Authorization": "Bearer " + accessToken,
	}

	body, err := m.httpClient.MakeRequestWithBody("POST", "https://auth.ddai.space/onchainTrigger", nil, headers)
	if err != nil {
		return fmt.Errorf("onchainTrigger request failed: %v", err)
	}

	var resp genericResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("failed to decode response: %v (body: %s)", err, string(body))
	}

	if resp.Status != "success" && resp.Status != "ok" {
		msg := "unknown error"
		if resp.Error != nil {
			if m, ok := resp.Error["message"]; ok {
				msg = fmt.Sprintf("%v", m)
			}
		}
		return fmt.Errorf("onchainTrigger failed: %s", msg)
	}

	return nil
}
