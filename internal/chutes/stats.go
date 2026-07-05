package chutes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

var statsClient = &http.Client{Timeout: 12 * time.Second}

func FetchModelStats(ctx context.Context) (map[string]map[string]interface{}, error) {
	var wg sync.WaitGroup
	wg.Add(2)

	var stats map[string]map[string]interface{}
	var statsErr error
	go func() {
		defer wg.Done()
		stats, statsErr = fetchHistoricalModelStats(ctx)
	}()

	var utilization map[string]map[string]interface{}
	var utilizationErr error
	go func() {
		defer wg.Done()
		utilization, utilizationErr = fetchModelUtilization(ctx)
	}()

	wg.Wait()
	if statsErr != nil && utilizationErr != nil {
		return nil, statsErr
	}
	if stats == nil {
		stats = map[string]map[string]interface{}{}
	}
	for name, util := range utilization {
		base := stats[name]
		if base == nil {
			base = map[string]interface{}{
				"chuteId":           util["chuteId"],
				"name":              name,
				"date":              "",
				"totalRequests":     0,
				"totalInputTokens":  0,
				"totalOutputTokens": 0,
				"averageTps":        0,
				"averageTtft":       0,
			}
		}
		for key, value := range util {
			base[key] = value
		}
		stats[name] = base
	}
	return stats, nil
}

func fetchHistoricalModelStats(ctx context.Context) (map[string]map[string]interface{}, error) {
	u, _ := url.Parse(DefaultAPIBase + "/invocations/stats/llm")
	q := u.Query()
	q.Set("start_date", isoDateDaysAgo(3))
	q.Set("end_date", isoDateDaysAgo(0))
	u.RawQuery = q.Encode()

	rows, err := fetchRows(ctx, u.String())
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]interface{}{}
	for _, row := range rows {
		name, _ := row["name"].(string)
		if name == "" || name == "[private]" {
			continue
		}
		current := out[name]
		date := stringField(row, "date")
		if current != nil && fmt.Sprint(current["date"]) >= date {
			continue
		}
		out[name] = map[string]interface{}{
			"chuteId":           stringField(row, "chute_id"),
			"name":              name,
			"date":              date,
			"totalRequests":     finiteNumber(row["total_requests"]),
			"totalInputTokens":  finiteNumber(row["total_input_tokens"]),
			"totalOutputTokens": finiteNumber(row["total_output_tokens"]),
			"averageTps":        finiteNumber(row["average_tps"]),
			"averageTtft":       finiteNumber(row["average_ttft"]),
		}
	}
	return out, nil
}

func fetchModelUtilization(ctx context.Context) (map[string]map[string]interface{}, error) {
	rows, err := fetchRows(ctx, DefaultAPIBase+"/chutes/utilization")
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]interface{}{}
	for _, row := range rows {
		name, _ := row["name"].(string)
		if name == "" || len(name) >= 8 && name[:8] == "[private" {
			continue
		}
		current := out[name]
		timestamp := stringField(row, "timestamp")
		if current != nil && fmt.Sprint(current["timestamp"]) >= timestamp {
			continue
		}
		out[name] = map[string]interface{}{
			"chuteId":              stringField(row, "chute_id"),
			"name":                 name,
			"timestamp":            timestamp,
			"activeInstanceCount":   finiteNumber(firstPresent(row, "active_instance_count", "instance_count")),
			"totalInstanceCount":    finiteNumber(firstPresent(row, "total_instance_count", "instance_count")),
			"utilizationCurrent":    finiteNumber(row["utilization_current"]),
			"utilization5m":         finiteNumber(row["utilization_5m"]),
			"utilization15m":        finiteNumber(row["utilization_15m"]),
			"utilization1h":         finiteNumber(row["utilization_1h"]),
			"rateLimitRatio5m":      finiteNumber(row["rate_limit_ratio_5m"]),
			"rateLimitRatio15m":     finiteNumber(row["rate_limit_ratio_15m"]),
			"rateLimitRatio1h":      finiteNumber(row["rate_limit_ratio_1h"]),
			"scalable":              boolField(row, "scalable"),
			"scaleAllowance":        finiteNumber(row["scale_allowance"]),
		}
	}
	return out, nil
}

func fetchRows(ctx context.Context, rawURL string) ([]map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json")
	res, err := statsClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("stats request failed: HTTP %d", res.StatusCode)
	}

	var decoded interface{}
	dec := json.NewDecoder(res.Body)
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		return nil, err
	}
	switch value := decoded.(type) {
	case []interface{}:
		return rowsFromInterfaceSlice(value), nil
	case map[string]interface{}:
		if data, ok := value["data"].([]interface{}); ok {
			return rowsFromInterfaceSlice(data), nil
		}
	}
	return []map[string]interface{}{}, nil
}

func rowsFromInterfaceSlice(values []interface{}) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(values))
	for _, value := range values {
		if row, ok := value.(map[string]interface{}); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func isoDateDaysAgo(days int) string {
	return time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")
}

func finiteNumber(value interface{}) float64 {
	switch v := value.(type) {
	case json.Number:
		f, _ := v.Float64()
		return f
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}

func firstPresent(row map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			return value
		}
	}
	return nil
}

func stringField(row map[string]interface{}, key string) string {
	if value, ok := row[key].(string); ok {
		return value
	}
	return fmt.Sprint(row[key])
}

func boolField(row map[string]interface{}, key string) bool {
	if value, ok := row[key].(bool); ok {
		return value
	}
	return false
}
