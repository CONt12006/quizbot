//go:build !solution

package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
)

const apiURL = "https://api.telegram.org/bot%s/%s"

type HTTPClient struct {
	token      string
	httpClient *http.Client
}

func NewHTTPClient(token string) *HTTPClient {
	return &HTTPClient{token: token, httpClient: &http.Client{}}
}

func (c *HTTPClient) SendMessage(chatID int64, text string, opts *SendOptions) (*Message, error) {
	params := map[string]interface{}{"chat_id": chatID, "text": text}
	applySendOptions(params, opts)
	raw, err := c.doRequest("sendMessage", params)
	if err != nil {
		return nil, err
	}
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (c *HTTPClient) EditMessage(chatID int64, messageID int, text string, opts *SendOptions) error {
	params := map[string]interface{}{"chat_id": chatID, "message_id": messageID, "text": text}
	applySendOptions(params, opts)
	_, err := c.doRequest("editMessageText", params)
	return err
}

func (c *HTTPClient) DeleteMessage(chatID int64, messageID int) error {
	_, err := c.doRequest("deleteMessage", map[string]interface{}{"chat_id": chatID, "message_id": messageID})
	return err
}

func (c *HTTPClient) AnswerCallback(callbackID string, text string) error {
	params := map[string]interface{}{"callback_query_id": callbackID}
	if text != "" {
		params["text"] = text
	}
	_, err := c.doRequest("answerCallbackQuery", params)
	return err
}

func (c *HTTPClient) GetUpdates(offset int, timeout int) ([]Update, error) {
	raw, err := c.doRequest("getUpdates", map[string]interface{}{"offset": offset, "timeout": timeout})
	if err != nil {
		return nil, err
	}
	var updates []Update
	if err := json.Unmarshal(raw, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func (c *HTTPClient) GetFile(fileID string) (string, error) {
	raw, err := c.doRequest("getFile", map[string]interface{}{"file_id": fileID})
	if err != nil {
		return "", err
	}
	var result struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if result.FilePath == "" {
		return "", fmt.Errorf("telegram returned empty file path")
	}
	return result.FilePath, nil
}

func (c *HTTPClient) DownloadFile(filePath string) ([]byte, error) {
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", c.token, filePath)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram file download failed: %s", resp.Status)
	}
	return data, nil
}

func (c *HTTPClient) SendDocument(chatID int64, fileName string, data []byte) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		return err
	}
	part, err := writer.CreateFormFile("document", fileName)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	url := fmt.Sprintf(apiURL, c.token, "sendDocument")
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"description"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("telegram api error: %s", result.Error)
	}
	return nil
}

func (c *HTTPClient) doRequest(method string, params map[string]interface{}) (json.RawMessage, error) {
	url := fmt.Sprintf(apiURL, c.token, method)
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"description"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram api error: %s", result.Error)
	}
	return result.Result, nil
}

func applySendOptions(params map[string]interface{}, opts *SendOptions) {
	if opts == nil {
		return
	}
	if opts.ParseMode != "" {
		params["parse_mode"] = opts.ParseMode
	}
	if opts.ReplyMarkup != nil {
		params["reply_markup"] = opts.ReplyMarkup
	}
}
