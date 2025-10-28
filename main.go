package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	assetsFile = "assets.json"
	zapBase    = "http://localhost:8080"
)

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Assets struct {
	List []Asset `json:"list"`
}

type Alert struct {
	Name     string   `json:"name"`
	URLs     []string `json:"urls"`
	Risk     string   `json:"risk"`
	Other    string   `json:"other,omitempty"`
	Solution string   `json:"solution,omitempty"`
}

func loadAssets() (Assets, error) {
	var a Assets
	file, err := os.Open(assetsFile)
	if os.IsNotExist(err) {
		return a, nil
	} else if err != nil {
		return a, err
	}
	defer file.Close()
	return a, json.NewDecoder(file).Decode(&a)
}

func saveAssets(a Assets) error {
	b, _ := json.MarshalIndent(a, "", "  ")
	return os.WriteFile(assetsFile, b, 0644)
}

func addAsset(name, url string) error {
	a, err := loadAssets()
	if err != nil {
		return err
	}
	for _, it := range a.List {
		if it.Name == name || it.URL == url {
			return fmt.Errorf("asset with same name or url already exists")
		}
	}
	a.List = append(a.List, Asset{Name: name, URL: url})
	return saveAssets(a)
}

func removeAsset(name string) error {
	a, err := loadAssets()
	if err != nil {
		return err
	}
	newList := []Asset{}
	found := false
	for _, it := range a.List {
		if it.Name == name {
			found = true
			fmt.Println("[i] clearing alerts in ZAP (best-effort)...")
			clearAllAlerts()
			continue
		}
		newList = append(newList, it)
	}
	if !found {
		return fmt.Errorf("asset not found: %s", name)
	}
	a.List = newList
	return saveAssets(a)
}

func listAssets() error {
	a, err := loadAssets()
	if err != nil {
		return err
	}
	if len(a.List) == 0 {
		fmt.Println("(no assets)")
		return nil
	}
	fmt.Println("Assets:")
	for _, it := range a.List {
		fmt.Printf("- %s -> %s\n", it.Name, it.URL)
	}
	return nil
}

func startScan(target string) (string, error) {
	endpoint := fmt.Sprintf("%s/JSON/ascan/action/scan/?url=%s&recurse=true&inScopeOnly=false", zapBase, url.QueryEscape(target))
	resp, err := http.Get(endpoint)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if v, ok := result["scan"]; ok {
		return fmt.Sprintf("%v", v), nil
	}
	if v, ok := result["Scan"]; ok {
		return fmt.Sprintf("%v", v), nil
	}
	if v, ok := result["Result"]; ok {
		return fmt.Sprintf("%v", v), nil
	}
	return "", fmt.Errorf("unexpected startScan response: %v", result)
}

func getScanStatus(scanId string) (int, error) {
	endpoint := fmt.Sprintf("%s/JSON/ascan/view/status/?scanId=%s", zapBase, url.QueryEscape(scanId))
	resp, err := http.Get(endpoint)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	if status, ok := result["status"]; ok {
		switch v := status.(type) {
		case string:
			var n int
			fmt.Sscanf(v, "%d", &n)
			return n, nil
		case float64:
			return int(v), nil
		}
	}
	for _, v := range result {
		if inner, ok := v.(map[string]interface{}); ok {
			if s, ok := inner["status"]; ok {
				switch t := s.(type) {
				case string:
					var n int
					fmt.Sscanf(t, "%d", &n)
					return n, nil
				case float64:
					return int(t), nil
				}
			}
		}
	}
	return 0, fmt.Errorf("could not parse status from response")
}

func getAlertsForBase(baseurl string) ([]map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/JSON/core/view/alerts/?baseurl=%s", zapBase, url.QueryEscape(baseurl))
	resp, err := http.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var wrapper map[string][]map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, err
	}
	if alerts, ok := wrapper["alerts"]; ok {
		return alerts, nil
	}
	return nil, nil
}

func clearAllAlerts() {
	endpoint := fmt.Sprintf("%s/JSON/core/action/deleteAllAlerts/", zapBase)
	http.Post(endpoint, "application/json", bytes.NewReader(nil))
}

func saveAlertsToFile(alerts []Alert, filename string) error {
	b, err := json.MarshalIndent(alerts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, b, 0644)
}

func groupAlertsByName(rawAlerts []map[string]interface{}) []Alert {
	m := make(map[string]*Alert)
	for _, a := range rawAlerts {
		name, _ := a["alert"].(string)
		urlStr, _ := a["url"].(string)
		risk, _ := a["risk"].(string)
		other, _ := a["other"].(string)
		solution, _ := a["solution"].(string)

		if name == "" {
			continue
		}
		if _, ok := m[name]; !ok {
			m[name] = &Alert{
				Name:     name,
				Risk:     risk,
				Other:    other,
				Solution: solution,
				URLs:     []string{},
			}
		}
		if !contains(m[name].URLs, urlStr) {
			m[name].URLs = append(m[name].URLs, urlStr)
		}
	}
	var result []Alert
	for _, v := range m {
		result = append(result, *v)
	}
	return result
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func prettyPrint(v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func usage() {
	fmt.Println("zapctl - small demo CLI")
	fmt.Println("Usage:")
	fmt.Println("  zapctl add -name NAME -url URL     # add asset")
	fmt.Println("  zapctl remove -name NAME           # remove asset (and clear alerts in ZAP)")
	fmt.Println("  zapctl list                        # list assets")
	fmt.Println("  zapctl scan -name NAME             # start active scan on asset (waits until 100%)")
	fmt.Println("  zapctl alerts -name NAME           # fetch alerts for asset (grouped by vulnerability)")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	cmd := os.Args[1]
	switch cmd {
	case "add":
		fs := flag.NewFlagSet("add", flag.ExitOnError)
		name := fs.String("name", "", "asset name")
		url := fs.String("url", "", "asset url")
		fs.Parse(os.Args[2:])
		if *name == "" || *url == "" {
			fmt.Println("name and url required")
			return
		}
		if err := addAsset(*name, *url); err != nil {
			fmt.Println("error:", err)
		} else {
			fmt.Println("added")
		}
	case "remove":
		fs := flag.NewFlagSet("remove", flag.ExitOnError)
		name := fs.String("name", "", "asset name")
		fs.Parse(os.Args[2:])
		if *name == "" {
			fmt.Println("name required")
			return
		}
		if err := removeAsset(*name); err != nil {
			fmt.Println("error:", err)
		} else {
			fmt.Println("removed")
		}
	case "list":
		if err := listAssets(); err != nil {
			fmt.Println("error:", err)
		}
	case "scan":
		fs := flag.NewFlagSet("scan", flag.ExitOnError)
		name := fs.String("name", "", "asset name")
		fs.Parse(os.Args[2:])
		if *name == "" {
			fmt.Println("name required")
			return
		}
		a, _ := loadAssets()
		var target string
		for _, it := range a.List {
			if it.Name == *name {
				target = it.URL
			}
		}
		if target == "" {
			fmt.Println("asset not found")
			return
		}
		fmt.Println("[i] starting active scan for", target)
		sid, err := startScan(target)
		if err != nil || sid == "" {
			fmt.Println("startScan error:", err)
			return
		}
		fmt.Println("[i] polling scan status...")
		for {
			status, err := getScanStatus(sid)
			if err != nil {
				fmt.Println("status poll err:", err)
				time.Sleep(2 * time.Second)
				continue
			}
			fmt.Printf("progress: %d%%\n", status)
			if status >= 100 {
				break
			}
			time.Sleep(2 * time.Second)
		}
		fmt.Println("[i] scan finished, fetching results...")
		rawAlerts, err := getAlertsForBase(target)
		if err != nil {
			fmt.Println("failed to fetch alerts:", err)
			return
		}
		grouped := groupAlertsByName(rawAlerts)
		timestamp := time.Now().Format("2006-01-02_15-04-05")
		filename := fmt.Sprintf("scan_results_%s_%s.json", *name, timestamp)
		if err := saveAlertsToFile(grouped, filename); err != nil {
			fmt.Println("failed to save results:", err)
		} else {
			fmt.Printf("[i] results saved to %s\n", filename)
		}
		fmt.Printf("Found %d grouped vulnerabilities:\n", len(grouped))
		for _, g := range grouped {
			fmt.Printf("- %s (%s) affecting %d URLs\n", g.Name, g.Risk, len(g.URLs))
			for _, u := range g.URLs {
				fmt.Printf("    * %s\n", u)
			}
		}
	case "alerts":
		fs := flag.NewFlagSet("alerts", flag.ExitOnError)
		name := fs.String("name", "", "asset name")
		fs.Parse(os.Args[2:])
		if *name == "" {
			fmt.Println("name required")
			return
		}
		a, _ := loadAssets()
		var target string
		for _, it := range a.List {
			if it.Name == *name {
				target = it.URL
			}
		}
		if target == "" {
			fmt.Println("asset not found")
			return
		}
		rawAlerts, err := getAlertsForBase(target)
		if err != nil {
			fmt.Println("error fetching alerts:", err)
			return
		}
		grouped := groupAlertsByName(rawAlerts)
		if len(grouped) == 0 {
			fmt.Println("(no alerts)")
			return
		}
		fmt.Printf("Found %d grouped vulnerabilities:\n", len(grouped))
		for _, g := range grouped {
			fmt.Printf("- %s (%s) affecting %d URLs\n", g.Name, g.Risk, len(g.URLs))
			if g.Other != "" {
				fmt.Printf("  Other: %s\n", g.Other)
			}
			if g.Solution != "" {
				fmt.Printf("  Solution: %s\n", g.Solution)
			}
			for _, u := range g.URLs {
				fmt.Printf("    * %s\n", u)
			}
		}
	default:
		usage()
	}
}
