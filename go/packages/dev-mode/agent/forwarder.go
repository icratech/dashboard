package agent

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"serverless/dev-mode-extension/lib"

	tags "buf.build/gen/go/serverless/sdk-schema/protocolbuffers/go/serverless/instrumentation/tags/v1"
	schema "buf.build/gen/go/serverless/sdk-schema/protocolbuffers/go/serverless/instrumentation/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

type LogMessage struct {
	Message    string `json:"message"`
	Timestamp  int64  `json:"timestamp"`
	SequenceId string `json:"sequenceId"`
	AccountId  string `json:"accountId"`
	RequestId  string `json:"requestId"`
}

type LogItem struct {
	Time     string      `json:"time"`
	LogType  string      `json:"type"`
	Metadata interface{} `json:"meta"`
	Record   interface{} `json:"record"`
}

type MetricsObject struct {
	DurationMs    uint32 `json:"durationMs"`
	ProducedBytes uint32 `json:"producedBytes"`
}

type InitReportRecord struct {
	InitializationType string        `json:"initializationType"`
	Metrics            MetricsObject `json:"metrics"`
	Phase              string        `json:"phase"`
}

type TelemetrySpans struct {
	Name       string `json:"name"`
	DurationMs uint32 `json:"durationMs"`
}

type RuntimeDoneRecord struct {
	RequestId string           `json:"requestId"`
	Status    string           `json:"status"`
	Spans     []TelemetrySpans `json:"spans"`
	Metrics   MetricsObject    `json:"metrics"`
}

var hasInternalExtension = lib.HasInternalExtension()

func FindRequestId(logs []LogItem) string {
	var requestId string = ""
	for _, log := range logs {
		if log.LogType == "platform.start" {
			record := log.Record.(map[string]interface{})
			requestId = record["requestId"].(string)
			break
		}
	}
	return requestId
}

func FindTraceId(logs []LogItem) string {
	var traceId string = ""

	// If we don't have the internal extension included
	// then we should generate one
	if !hasInternalExtension {
		return uuid.NewString()
	}

	for _, log := range logs {
		if log.LogType == "spans" {
			if rawPayload, decodeErr := base64.StdEncoding.DecodeString(log.Record.(string)); decodeErr == nil {
				var devModePayload schema.TracePayload
				traceErr := proto.Unmarshal(rawPayload, &devModePayload)
				if traceErr == nil && devModePayload.Spans != nil && len(devModePayload.Spans) > 0 && devModePayload.Spans[0] != nil {
					traceId = base64.StdEncoding.EncodeToString(devModePayload.Spans[0].TraceId)
					break
				} else if traceErr == nil && devModePayload.Events != nil && len(devModePayload.Events) > 0 && devModePayload.Events[0] != nil {
					traceId = base64.StdEncoding.EncodeToString(devModePayload.Events[0].TraceId)
					break
				}
			}
		}
	}
	return traceId
}

func FindInitReport(logs []LogItem) *LogItem {
	for _, log := range logs {
		if log.LogType == "platform.initReport" {
			return &log
		}
	}
	return nil
}

func FindPlatformStart(logs []LogItem) *LogItem {
	for _, log := range logs {
		if log.LogType == "platform.start" {
			return &log
		}
	}
	return nil
}

func FindRuntimeDone(logs []LogItem) *LogItem {
	for _, log := range logs {
		if log.LogType == "platform.runtimeDone" {
			return &log
		}
	}
	return nil
}

func stringIncludesInitError(str string) bool {
	return strings.Contains(str, "\tundefined\tERROR") && strings.Contains(str, "\tundefined\tUncaught Exception")
}

func FindInitErrorLog(logs []LogItem) *LogItem {
	for _, log := range logs {
		if log.LogType == "function" && stringIncludesInitError(log.Record.(string)) {
			return &log
		}
	}
	return nil
}

func FindResData(logs []LogItem) *LogItem {
	for _, log := range logs {
		if log.LogType == "reqRes" {
			if rawPayload, decodeErr := base64.StdEncoding.DecodeString(log.Record.(string)); decodeErr == nil {
				var reqResPayload schema.RequestResponse
				reqResErr := proto.Unmarshal(rawPayload, &reqResPayload)
				if reqResErr == nil {
					origin := reqResPayload.GetOrigin().String()
					if origin == "ORIGIN_RESPONSE" {
						return &log
					}
				}
			}
		}
	}
	return nil
}

func FindReqData(logs []LogItem) *LogItem {
	for _, log := range logs {
		if log.LogType == "reqRes" {
			if rawPayload, decodeErr := base64.StdEncoding.DecodeString(log.Record.(string)); decodeErr == nil {
				var reqResPayload schema.RequestResponse
				reqResErr := proto.Unmarshal(rawPayload, &reqResPayload)
				if reqResErr == nil {
					origin := reqResPayload.GetOrigin().String()
					if origin == "ORIGIN_REQUEST" {
						return &log
					}
				}
			}
		}
	}
	return nil
}

func IsCapturedLogMessage(record interface{}) bool {
	message := fmt.Sprintf("%v", record)

	// Not enough items in the list
	// The first item is not a date timestamp
	// The second item is not a request id
	// The third item is not WARN or ERROR
	date, requestId, logLevel := extractDefaultMessageValues(message)
	if !hasInternalExtension || !IsValidDate(date) || !IsValidUUID(requestId) || !IsWarnOrError(logLevel) {
		return isLoggerWarningOrError(message)
	}
	return true
}

func isLoggerWarningOrError(message string) bool {
	runtime := lib.InternalExtensionRuntime()
	switch runtime {
	case "node":
		var jsonObject map[string]interface{}
		err := json.Unmarshal([]byte(message), &jsonObject)
		if err != nil || jsonObject["level"] == nil {
			return false
		}

		processedLevel := ProcessLoggerLevel(jsonObject["level"])
		return IsWarnOrError(processedLevel)
	default:
		return false
	}
}

func ProcessLoggerLevel(level interface{}) string {
	// convert level to int
	if levelInt, ok := level.(int); ok {
		if levelInt <= 30 {
			return ""
		} else if levelInt <= 40 {
			return "WARN"
		}
		return "ERROR"
	}
	return fmt.Sprintf("%v", level)
}

// date, requestId, logLevel
func extractDefaultMessageValues(message string) (string, string, string) {
	arr := strings.Split(message, "\t")
	logLevel := ""
	date := ""
	requestId := ""
	if len(arr) < 4 {
		return date, requestId, logLevel
	}
	runtime := lib.InternalExtensionRuntime()
	switch runtime {
	case "node":
		logLevel = arr[2]
		requestId = arr[1]
		date = arr[0]
		return date, requestId, logLevel
	case "python":
		requestId = arr[2]
		date = arr[1]
		removeLeadingBrace := strings.Replace(arr[0], "[", "", 1)
		logLevel = strings.Replace(removeLeadingBrace, "]", "", 1)
		return date, requestId, logLevel
	default:
		return date, requestId, logLevel
	}
}

func IsWarnOrError(logLevel string) bool {
	if logLevel == "WARNING" || logLevel == "WARN" || logLevel == "ERROR" {
		return true
	}
	return false
}

func IsValidDate(date string) bool {
	_, isValid := time.Parse("2006-01-02T15:04:05.000Z", date)
	return isValid == nil
}

func IsValidUUID(uuid string) bool {
	r := regexp.MustCompile("^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-4[a-fA-F0-9]{3}-[8|9|aA|bB][a-fA-F0-9]{3}-[a-fA-F0-9]{12}$")
	return r.MatchString(uuid)
}

func FormatLogs(logs []LogItem, requestId string, accountId string, traceId string) schema.LogPayload {
	messages := make([]*schema.LogEvent, 0)
	platform := "aws"
	region := os.Getenv("AWS_REGION")
	slsTags := tags.SlsTags{
		OrgId:    os.Getenv("SLS_DEV_MODE_ORG_ID"),
		Platform: &platform,
		Region:   &region,
		Service:  os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
	}
	payload := schema.LogPayload{
		SlsTags:   &slsTags,
		LogEvents: messages,
	}
	if IsValidUUID(traceId) {
		traceId = base64.StdEncoding.EncodeToString([]byte(traceId))
	}
	for i, log := range logs {
		if log.LogType == "function" && !IsCapturedLogMessage(log.Record) {
			logTime := time.Now()
			if t, timeParseError := time.Parse(time.RFC3339, log.Time); timeParseError == nil {
				logTime = t
			}
			if !strings.Contains(log.Record.(string), "SERVERLESS_TELEMETRY.") {
				// Apparently these environment variables don't
				// exist in the extension 🤷‍♂️
				logGroup := os.Getenv("AWS_LAMBDA_LOG_GROUP_NAME")
				logStream := os.Getenv("AWS_LAMBDA_LOG_STREAM_NAME")
				orgId := os.Getenv("SLS_DEV_MODE_ORG_ID")
				rec := log.Record.(string)
				sequenceId := fmt.Sprintf("%v", time.Now().UnixNano()+int64(i))
				messages = append(messages, &schema.LogEvent{
					Body:      rec,
					Timestamp: uint64(logTime.UnixMilli()),
					// Allow the backend to handle setting
					// the severity levels
					SeverityText:   "Info",
					SeverityNumber: uint64(1),
					TraceId:        &traceId,
					Tags: &tags.Tags{
						Aws: &tags.AwsTags{
							LogGroup:   &logGroup,
							LogStream:  &logStream,
							SequenceId: &sequenceId,
							AccountId:  &accountId,
							RequestId:  &requestId,
						},
						OrgId: &orgId,
					},
				})
			}
		}
	}
	payload.LogEvents = messages
	return payload
}

func CollectRequestResponseData(logs []LogItem, requestId string, accountId string, traceId string) ([][]byte, []*LogItem) {
	messages := make([][]byte, 0)
	metadata := make([]*LogItem, 0)
	hasPlatformStart := FindPlatformStart(logs)
	hasRuntimeDone := FindRuntimeDone(logs)
	foundReqRes := false
	for _, log := range logs {
		if log.LogType == "reqRes" {
			foundReqRes = true
			if jsonString, jsonError := json.Marshal(log.Metadata); jsonError == nil {
				meta := LogItem{}
				json.Unmarshal(jsonString, &meta)
				metadata = append(metadata, &meta)
				/* Parse req/res data and add tags we are adding below
				   Aws: &tags.AwsTags{
				   	AccountId:    &accountId,
				   	Region:       &region,
				   	RequestId:    &requestId,
				   	ResourceName: &functionName,
				   },
				   OrgId: &orgId,
				   Sdk: &tags.SdkTags{
				   	Name:    "@serverless/external-extension",
				   	Version: "N/A",
				   }, */
				messages = append(messages, []byte(log.Record.(string)))
			}
		}
	}

	// Generate the req event if the SDK is not enabled and if we receive the platform.start event.
	if !foundReqRes && hasPlatformStart != nil && !hasInternalExtension {
		isHistorical := false
		body := ""
		payloadType := "aws-lambda-request"
		reqTime := time.Now()
		if r, timeParseError := time.Parse("2006-01-02T15:04:05.000Z", hasPlatformStart.Time); timeParseError == nil {
			reqTime = r
		}
		epoch := uint64(reqTime.UnixNano())
		orgId := os.Getenv("SLS_DEV_MODE_ORG_ID")
		region := os.Getenv("AWS_REGION")
		functionName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")
		slsTagPlatform := "lambda"
		reqProto := &schema.RequestResponse{
			SlsTags: &tags.SlsTags{
				OrgId: orgId,
				Sdk: &tags.SdkTags{
					Name:    "@serverless/external-extension",
					Version: "N/A",
				},
				Platform: &slsTagPlatform,
				Region:   &region,
				Service:  functionName,
			},
			IsHistorical: &isHistorical,
			Body:         &body,
			RequestId:    &requestId,
			SpanId:       []byte(requestId),
			Origin:       schema.RequestResponse_ORIGIN_REQUEST,
			Timestamp:    &epoch,
			Type:         &payloadType,
			TraceId:      []byte(traceId),
			Tags: &tags.Tags{
				Aws: &tags.AwsTags{
					AccountId:    &accountId,
					Region:       &region,
					RequestId:    &requestId,
					ResourceName: &functionName,
				},
				OrgId: &orgId,
				Sdk: &tags.SdkTags{
					Name:    "@serverless/external-extension",
					Version: "N/A",
				},
			},
		}
		reqData, err := proto.Marshal(reqProto)
		if err == nil {
			messages = append(messages, []byte(base64.StdEncoding.EncodeToString(reqData)))
		}
	}
	// Generate the res event - if we receive the runtime done event and have not received the res event
	if !foundReqRes && hasRuntimeDone != nil {
		isHistorical := false
		body := ""
		payloadType := "aws-lambda-response"
		reqTime := time.Now()
		if r, timeParseError := time.Parse("2006-01-02T15:04:05.000Z", hasRuntimeDone.Time); timeParseError == nil {
			reqTime = r
		}
		epoch := uint64(reqTime.UnixNano())
		orgId := os.Getenv("SLS_DEV_MODE_ORG_ID")
		region := os.Getenv("AWS_REGION")
		functionName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")
		slsTagPlatform := "lambda"

		var errorTag tags.ErrorTags
		recordAsJSON := hasRuntimeDone.Record.(map[string]interface{})
		if recordAsJSON["status"] == "timeout" {
			metrics := recordAsJSON["metrics"].(map[string]interface{})
			durationInMS := metrics["durationMs"]
			message := fmt.Sprintf("Task timed out after %.2f milliseconds", durationInMS)
			errorTag = tags.ErrorTags{
				Type:    tags.ErrorTags_ERROR_TYPE_UNCAUGHT,
				Name:    "function_timeout",
				Message: &message,
			}
		} else if recordAsJSON["status"] == "error" {
			message := fmt.Sprintf("Received %s. This is most likely an initialization error", recordAsJSON["errorType"])
			errorTag = tags.ErrorTags{
				Type:    tags.ErrorTags_ERROR_TYPE_UNCAUGHT,
				Name:    "function_init_error",
				Message: &message,
			}
		}

		reqProto := &schema.RequestResponse{
			SlsTags: &tags.SlsTags{
				OrgId: orgId,
				Sdk: &tags.SdkTags{
					Name:    "@serverless/external-extension",
					Version: "N/A",
				},
				Platform: &slsTagPlatform,
				Region:   &region,
				Service:  functionName,
			},
			IsHistorical: &isHistorical,
			Body:         &body,
			RequestId:    &requestId,
			SpanId:       []byte(requestId),
			Origin:       schema.RequestResponse_ORIGIN_RESPONSE,
			Timestamp:    &epoch,
			Type:         &payloadType,
			TraceId:      []byte(traceId),
			Tags: &tags.Tags{
				Aws: &tags.AwsTags{
					AccountId:    &accountId,
					Region:       &region,
					RequestId:    &requestId,
					ResourceName: &functionName,
				},
				Error: &errorTag,
				OrgId: &orgId,
				Sdk: &tags.SdkTags{
					Name:    "@serverless/external-extension",
					Version: "N/A",
				},
			},
		}
		reqData, err := proto.Marshal(reqProto)
		if err == nil {
			messages = append(messages, []byte(base64.StdEncoding.EncodeToString(reqData)))
		}
	}

	return messages, metadata
}

func CollectTraceData(logs []LogItem) [][]byte {
	messages := make([][]byte, 0)
	for _, log := range logs {
		if log.LogType == "spans" {
			messages = append(messages, []byte(log.Record.(string)))
		}
	}
	return messages
}

func ForwardActivity(payloads []schema.DevModeTransportPayload) (int, error) {
	path := "/dev"
	if len(payloads) == 0 {
		return 200, nil
	}
	finalPayload := schema.DevModeTransportPayload{
		AccountId: payloads[0].AccountId,
		Region:    payloads[0].Region,
		RequestId: payloads[0].RequestId,
	}
	for _, activity := range payloads {
		finalPayload.Logs = append(finalPayload.Logs, activity.Logs...)
		finalPayload.RequestResponse = append(finalPayload.RequestResponse, activity.RequestResponse...)
		finalPayload.Traces = append(finalPayload.Traces, activity.Traces...)
	}
	// If we aggregate all of the data together and there is no data to send we should just return here rather
	// than forwarding an empty payload
	if (len(finalPayload.Logs) == 0) && len(finalPayload.RequestResponse) == 0 && len(finalPayload.Traces) == 0 {
		return 200, nil
	}
	if body, protoMarshalError := proto.Marshal(&finalPayload); protoMarshalError == nil {
		// Send data to backends
		var _, internalLogsOnly = os.LookupEnv("SLS_TEST_EXTENSION_INTERNAL_LOG")
		var _, toLogs = os.LookupEnv("SLS_TEST_EXTENSION_LOG")
		// Gzip the body
		var gzipBody bytes.Buffer
		gz := gzip.NewWriter(&gzipBody)
		if _, err := gz.Write(body); err != nil {
			return 500, nil
		}
		gz.Close()

		// If we are running integration tests we just want to write the JSON payloads to CW
		if toLogs {
			lib.ReportDevModePayload(gzipBody.String())
			return 200, nil
		} else {
			url := lib.GetBaseUrl() + path
			lib.Info("Publish URL", url)
			// If we are running unit tests we want to publish logs to the local testing server
			if internalLogsOnly {
				extensions_api_address, ok := os.LookupEnv("AWS_LAMBDA_RUNTIME_API")
				if !ok {
					lib.Error("AWS_LAMBDA_RUNTIME_API is not set")
				}
				url = fmt.Sprintf("http://%s/save"+path, extensions_api_address)
			}

			token, _ := os.LookupEnv("SLS_DEV_TOKEN")
			client := &http.Client{}
			if req, newRequestError := http.NewRequest("POST", url, &gzipBody); newRequestError == nil {
				req.Header.Add("Content-Type", "application/gzip")
				req.Header.Add("Authorization", "Bearer "+token)
				req.Header.Add("sls-token-type", "orgToken")
				res, resErr := client.Do(req)
				if resErr != nil {
					lib.Error("API Call failed", resErr)
					return 500, resErr
				}
				return res.StatusCode, resErr
			}
		}
	}
	return 200, nil
}

func AggregateActivity(logs []LogItem, requestId string, accountId string, traceId string) *schema.DevModeTransportPayload {
	region := os.Getenv("AWS_REGION")
	aggregatedActivity := schema.DevModeTransportPayload{
		AccountId: accountId,
		Region:    region,
		RequestId: requestId,
	}
	// Send reqRes payloads
	payloads, metadata := CollectRequestResponseData(logs, requestId, accountId, traceId)
	if len(payloads) != 0 {
		for index, payload := range payloads {
			functionName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")
			if rawPayload, decodeErr := base64.StdEncoding.DecodeString(string(payload)); decodeErr == nil {
				var devModePayload schema.RequestResponse
				reqResErr := proto.Unmarshal(rawPayload, &devModePayload)
				if reqResErr == nil {
					// Attach metadata from the Lambda Telemetry API
					// to the finalPayload
					if len(metadata) > index {
						meta := metadata[index]
						// Update the response payload so that it uses the
						// time from the platform.runtimeDone event
						if meta.Time != "" && devModePayload.Origin == schema.RequestResponse_ORIGIN_RESPONSE {
							metaTime, err := time.Parse("2006-01-02T15:04:05.000Z", meta.Time)
							if err == nil {
								epoch := uint64(metaTime.UnixNano())
								devModePayload.Timestamp = &epoch
							}
						}
						if meta.LogType == "platform.initReport" {
							if jsonString, jsonErr := json.Marshal(meta.Record); jsonErr == nil {
								reportData := InitReportRecord{}
								json.Unmarshal(jsonString, &reportData)
							}
						} else if meta.LogType == "platform.runtimeDone" {
							if jsonString, jsonErr := json.Marshal(meta.Record); jsonErr == nil {
								reportData := RuntimeDoneRecord{}
								json.Unmarshal(jsonString, &reportData)
							}
						}
					}

					if devModePayload.Tags == nil {
						devModePayload.Tags = &tags.Tags{}
					}
					devModePayload.Tags.Aws = &tags.AwsTags{
						AccountId:    &accountId,
						RequestId:    &requestId,
						Region:       &region,
						ResourceName: &functionName,
					}

					aggregatedActivity.RequestResponse = append(aggregatedActivity.RequestResponse, &devModePayload)
				} else {
					lib.Info("Proto Error", reqResErr)
				}
			}
		}
	}

	// Send trace payloads
	tracePayloads := CollectTraceData(logs)
	if len(tracePayloads) != 0 {
		for _, payload := range tracePayloads {
			if rawPayload, decodeErr := base64.StdEncoding.DecodeString(string(payload)); decodeErr == nil {
				var devModePayload schema.TracePayload
				traceErr := proto.Unmarshal(rawPayload, &devModePayload)
				if traceErr == nil {
					aggregatedActivity.Traces = append(aggregatedActivity.Traces, &devModePayload)
				}
			}
		}
	}

	logPayload := FormatLogs(logs, requestId, accountId, traceId)
	if len(logPayload.LogEvents) != 0 {
		aggregatedActivity.Logs = append(aggregatedActivity.Logs, &logPayload)
	}

	return &aggregatedActivity
}
