package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

func (t *BrowserTool) doCreateSession(ctx context.Context, in *Input) string {
	s, err := t.manager.CreateSession(ctx, in)
	if err != nil {
		return errStr(err)
	}
	p := s.activePage()
	url := ""
	if p != nil {
		url = p.MustInfo().URL
	}
	return fmt.Sprintf("Session created.\n  session_id: %s\n  headless: %v\n  url: %s\n  tab: %s",
		s.ID, s.Headless, url, s.activeTab)
}

func (t *BrowserTool) doCloseSession(in *Input) string {
	if in.SessionID == "" {
		return "Error: session_id is required for close_session"
	}
	if err := t.manager.CloseSession(in.SessionID); err != nil {
		return errStr(err)
	}
	return fmt.Sprintf("Session %s closed.", in.SessionID)
}

func (t *BrowserTool) doListSessions() string {
	list := t.manager.ListSessions()
	if len(list) == 0 {
		return "No active browser sessions."
	}
	return resultJSON(list)
}

func (t *BrowserTool) doNavigate(in *Input) string {
	s, page, err := t.getSessionAndPage(in)
	if err != nil {
		return errStr(err)
	}
	if in.URL == "" {
		return "Error: url is required for navigate"
	}

	timeout := 30 * time.Second
	if in.Timeout > 0 {
		timeout = time.Duration(in.Timeout) * time.Millisecond
	}

	page = page.Timeout(timeout)

	err = page.Navigate(in.URL)
	if err != nil {
		return fmt.Sprintf("Navigate failed: %v", err)
	}

	// Wait based on load mode
	switch s.loadMode {
	case "none":
		// Don't wait
	case "eager":
		_ = page.WaitDOMStable(500*time.Millisecond, 0.1)
	default: // "normal"
		err = page.WaitLoad()
		if err != nil {
			return fmt.Sprintf("Navigate succeeded but WaitLoad failed: %v\nURL: %s", err, page.MustInfo().URL)
		}
	}

	info := page.MustInfo()
	return fmt.Sprintf("Navigated successfully.\n  URL: %s\n  Title: %s", info.URL, info.Title)
}

func (t *BrowserTool) doNavDirection(in *Input, direction string) string {
	_, page, err := t.getSessionAndPage(in)
	if err != nil {
		return errStr(err)
	}

	switch direction {
	case "back":
		err = page.NavigateBack()
	case "forward":
		err = page.NavigateForward()
	case "reload":
		err = proto.PageReload{}.Call(page)
	}

	if err != nil {
		return fmt.Sprintf("%s failed: %v", direction, err)
	}

	_ = page.WaitLoad()
	info := page.MustInfo()
	return fmt.Sprintf("%s done.\n  URL: %s\n  Title: %s", direction, info.URL, info.Title)
}

func (t *BrowserTool) doWaitForLoad(in *Input) string {
	_, page, err := t.getSessionAndPage(in)
	if err != nil {
		return errStr(err)
	}

	timeout := 30 * time.Second
	if in.Timeout > 0 {
		timeout = time.Duration(in.Timeout) * time.Millisecond
	}

	err = page.Timeout(timeout).WaitLoad()
	if err != nil {
		return fmt.Sprintf("wait_for_load timed out: %v", err)
	}

	info := page.MustInfo()
	return fmt.Sprintf("Page loaded.\n  URL: %s\n  Title: %s", info.URL, info.Title)
}

// resolveElement finds an element using the locator from Input.
func (t *BrowserTool) resolveElement(page *rod.Page, in *Input) (*rod.Element, error) {
	if in.Locator == "" {
		return nil, fmt.Errorf("locator is required")
	}

	resolved := Resolve(in.Locator)

	timeout := 10 * time.Second
	if in.Timeout > 0 {
		timeout = time.Duration(in.Timeout) * time.Millisecond
	}
	page = page.Timeout(timeout)

	switch resolved.Strategy {
	case StrategyXPath:
		return page.ElementX(resolved.Value)
	default:
		return page.Element(resolved.Value)
	}
}

// resolveElements finds multiple elements using the locator.
func (t *BrowserTool) resolveElements(page *rod.Page, in *Input) (rod.Elements, error) {
	if in.Locator == "" {
		return nil, fmt.Errorf("locator is required")
	}

	resolved := Resolve(in.Locator)

	switch resolved.Strategy {
	case StrategyXPath:
		return page.ElementsX(resolved.Value)
	default:
		return page.Elements(resolved.Value)
	}
}
