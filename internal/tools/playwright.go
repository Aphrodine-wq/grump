package tools

import (
	"fmt"
	"strings"
	"sync"

	"github.com/playwright-community/playwright-go"
)

var (
	pw         *playwright.Playwright
	pwBrowser  playwright.Browser
	pwPage     playwright.Page
	pwLock     sync.Mutex
	pwInitOnce sync.Once
)

func initPlaywright() error {
	var err error
	pwInitOnce.Do(func() {
		// Suppress stdout for install unless error
		err = playwright.Install()
		if err != nil {
			err = fmt.Errorf("could not install playwright browsers: %w", err)
			return
		}
		
		pw, err = playwright.Run()
		if err != nil {
			err = fmt.Errorf("could not start playwright: %w", err)
			return
		}

		pwBrowser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
			Headless: playwright.Bool(true),
		})
		if err != nil {
			err = fmt.Errorf("could not launch browser: %w", err)
			return
		}

		pwPage, err = pwBrowser.NewPage()
		if err != nil {
			err = fmt.Errorf("could not create page: %w", err)
			return
		}
	})
	return err
}

func ToolBrowserAction(args map[string]interface{}) (string, error) {
	action := str(args, "action")
	
	pwLock.Lock()
	defer pwLock.Unlock()

	if err := initPlaywright(); err != nil {
		return "", err
	}

	switch action {
	case "goto":
		url := str(args, "url")
		if !strings.HasPrefix(url, "http") {
			url = "http://" + url
		}
		if _, err := pwPage.Goto(url, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateNetworkidle,
		}); err != nil {
			return "", fmt.Errorf("failed to navigate: %v", err)
		}
		title, _ := pwPage.Title()
		return fmt.Sprintf("Navigated to %s. Title: %s", url, title), nil

	case "click":
		selector := str(args, "selector")
		if err := pwPage.Click(selector); err != nil {
			return "", fmt.Errorf("failed to click %s: %v", selector, err)
		}
		return fmt.Sprintf("Clicked element: %s", selector), nil

	case "fill":
		selector := str(args, "selector")
		text := str(args, "text")
		if err := pwPage.Fill(selector, text); err != nil {
			return "", fmt.Errorf("failed to fill %s: %v", selector, err)
		}
		return fmt.Sprintf("Filled %s with text.", selector), nil

	case "evaluate":
		script := str(args, "script")
		result, err := pwPage.Evaluate(script)
		if err != nil {
			return "", fmt.Errorf("evaluation failed: %v", err)
		}
		return fmt.Sprintf("Script Result: %v", result), nil

	case "extract_text":
		// Strips heavy DOM down to readable text
		loc := pwPage.Locator("body")
		text, err := loc.InnerText()
		if err != nil {
			return "", fmt.Errorf("failed to extract text: %v", err)
		}
		if len(text) > 10000 {
			text = text[:10000] + "\n... [truncated]"
		}
		return fmt.Sprintf("Page Text:\n%s", text), nil

	case "close":
		pwBrowser.Close()
		pw.Stop()
		// Reset sync.Once is hard in Go, so we'll just exit the process cleanly if needed or ignore it.
		// For a persistent CLI, we'll just let it run.
		return "Browser closed.", nil

	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}