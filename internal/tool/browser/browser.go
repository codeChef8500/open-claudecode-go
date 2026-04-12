package browser

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/tool"
)

// BrowserTool implements engine.Tool for DrissionPage-style browser automation.
type BrowserTool struct {
	tool.BaseTool
	manager *SessionManager
}

// New creates a new BrowserTool ready to register.
func New() *BrowserTool {
	return &BrowserTool{
		manager: getManager(),
	}
}

func (t *BrowserTool) Name() string           { return "BrowserDrission" }
func (t *BrowserTool) UserFacingName() string { return "Browser" }

func (t *BrowserTool) Description() string {
	return "DrissionPage-style advanced browser automation tool (Go + rod). " +
		"Supports 100+ actions: session management, navigation, element location (CSS/XPath/text/attr), " +
		"smart click with 3-level fallback, network listening, cookie/auth injection, " +
		"Cloudflare bypass, Google CAPTCHA/consent bypass, CDP commands, screenshot/PDF, multi-tab, and more."
}

func (t *BrowserTool) InputSchema() json.RawMessage       { return inputSchema() }
func (t *BrowserTool) IsEnabled(_ *tool.UseContext) bool  { return true }
func (t *BrowserTool) MaxResultSizeChars() int            { return 200_000 }
func (t *BrowserTool) IsOpenWorld(_ json.RawMessage) bool { return true }

func (t *BrowserTool) Prompt(_ *tool.UseContext) string {
	return `Browser automation tool with DrissionPage-style locators.

Use "action" to specify what to do. Common workflow:
  1. create_session — launch browser (headless by default)
  2. navigate — go to URL
  3. find_element / smart_click / input — interact with page
  4. screenshot / get_html / snapshot — extract data
  5. close_session — release resources

Locator syntax (set via "locator" field):
  CSS:       #id  .class  css=div>span
  XPath:     //div[@id='x']  xpath=//a
  Text:      text=Login  text:Search  text^Start  text$End
  Attribute: @href=/api  @@class=btn@@type=submit
  Tag+attr:  tag:button@type=submit

Key actions:
  Session:   create_session, close_session, list_sessions
  Navigate:  navigate, back, forward, reload
  Elements:  find_element, find_elements, smart_click, hover, input
  Wait:      wait_for_element, wait_for_url, wait_for_network_idle
  Network:   network_listen_start, network_listen_wait, network_listen_get
  Data:      get_cookies, set_cookies, screenshot, pdf, get_html, snapshot
  Auth:      set_extra_headers, inject_cookies_string, inject_auth_token
  CDP:       cdp_send, set_geolocation, set_timezone
  CF bypass: wait_cloudflare_challenge, extract_cf_clearance
  Google:    detect_google_captcha, wait_google_challenge, handle_google_consent, inject_google_cookies

Google Search anti-block workflow (recommended):
  1. create_session(headless=false, user_data_dir="~/.chrome-profile")
  2. navigate(url="https://www.google.com/search?q=...", google_auto_consent=true)
  3. detect_google_captcha → check if blocked
  4. wait_google_challenge(google_challenge_timeout=60000) → wait/handle if blocked
  5. find_elements / get_html / snapshot → extract results`
}

func (t *BrowserTool) GetActivityDescription(input json.RawMessage) string {
	var in Input
	if err := json.Unmarshal(input, &in); err != nil {
		return "Browser action"
	}
	switch in.Action {
	case ActionNavigate:
		u := in.URL
		if len(u) > 60 {
			u = u[:60] + "…"
		}
		return "Navigating to " + u
	case ActionScreenshot:
		return "Taking screenshot"
	case ActionSmartClick:
		return "Clicking " + in.Locator
	default:
		return "Browser: " + in.Action
	}
}

func (t *BrowserTool) ValidateInput(_ context.Context, input json.RawMessage) error {
	var in Input
	if err := json.Unmarshal(input, &in); err != nil {
		return fmt.Errorf("invalid input JSON: %w", err)
	}
	if in.Action == "" {
		return fmt.Errorf("action is required")
	}
	return nil
}

func (t *BrowserTool) CheckPermissions(_ context.Context, _ json.RawMessage, _ *tool.UseContext) error {
	return nil
}

// Call dispatches the browser action and returns results via channel.
func (t *BrowserTool) Call(ctx context.Context, input json.RawMessage, uctx *tool.UseContext) (<-chan *engine.ContentBlock, error) {
	var in Input
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	ch := make(chan *engine.ContentBlock, 2)
	go func() {
		defer close(ch)
		result := t.dispatch(ctx, &in)
		ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: result}
	}()
	return ch, nil
}

// dispatch routes the action to the appropriate handler.
func (t *BrowserTool) dispatch(ctx context.Context, in *Input) string {
	switch in.Action {
	// --- Session management ---
	case ActionCreateSession:
		return t.doCreateSession(ctx, in)
	case ActionCloseSession:
		return t.doCloseSession(in)
	case ActionListSessions:
		return t.doListSessions()

	// --- Navigation ---
	case ActionNavigate:
		return t.doNavigate(in)
	case ActionBack:
		return t.doNavDirection(in, "back")
	case ActionForward:
		return t.doNavDirection(in, "forward")
	case ActionReload:
		return t.doNavDirection(in, "reload")
	case ActionWaitForLoad:
		return t.doWaitForLoad(in)

	// --- Element locating ---
	case ActionFindElement:
		return t.doFindElement(in)
	case ActionFindElements:
		return t.doFindElements(in)
	case ActionGetElementInfo:
		return t.doGetElementInfo(in)
	case ActionGetElementState:
		return t.doGetElementState(in)

	// --- Smart interaction ---
	case ActionSmartClick:
		return t.doSmartClick(in)
	case ActionHover:
		return t.doHover(in)
	case ActionDoubleClick:
		return t.doDoubleClick(in)
	case ActionRightClick:
		return t.doRightClick(in)
	case ActionDragDrop:
		return t.doDragDrop(in)
	case ActionSelectOpt:
		return t.doSelectOption(in)
	case ActionUploadFile:
		return t.doUploadFile(in)
	case ActionExecuteJS:
		return t.doExecuteJS(in)
	case ActionKeyPress:
		return t.doKeyPress(in)
	case ActionClearInput:
		return t.doClearInput(in)
	case ActionInput:
		return t.doInput(in)

	// --- Wait ---
	case ActionWaitForElement:
		return t.doWaitForElement(in)
	case ActionWaitForURL:
		return t.doWaitForURL(in)
	case ActionWaitForTitle:
		return t.doWaitForTitle(in)
	case ActionWaitForNetworkIdle:
		return t.doWaitForNetworkIdle(in)
	case ActionWaitForAlert:
		return t.doWaitForAlert(in)
	case ActionWaitForAnyElement:
		return t.doWaitForAnyElement(in)

	// --- Network listening ---
	case ActionNetworkListenStart:
		return t.doNetworkListenStart(in)
	case ActionNetworkListenWait:
		return t.doNetworkListenWait(in)
	case ActionNetworkListenGet:
		return t.doNetworkListenGet(in)
	case ActionNetworkListenStop:
		return t.doNetworkListenStop(in)
	case ActionNetworkListenClear:
		return t.doNetworkListenClear(in)
	case ActionNetworkListenSteps:
		return t.doNetworkListenSteps(in)

	// --- Dialog ---
	case ActionHandleAlert:
		return t.doHandleAlert(in)
	case ActionGetAlertText:
		return t.doGetAlertText(in)
	case ActionSetAutoAlert:
		return t.doSetAutoAlert(in)

	// --- IFrame ---
	case ActionListIframes:
		return t.doListIframes(in)
	case ActionEnterIframe:
		return t.doEnterIframe(in)
	case ActionExitIframe:
		return t.doExitIframe(in)

	// --- Tabs ---
	case ActionNewTab:
		return t.doNewTab(in)
	case ActionListTabs:
		return t.doListTabs(in)
	case ActionSwitchTab:
		return t.doSwitchTab(in)
	case ActionCloseTab:
		return t.doCloseTab(in)
	case ActionClickForNewTab:
		return t.doClickForNewTab(in)
	case ActionClickForURLChange:
		return t.doClickForURLChange(in)

	// --- Data extraction ---
	case ActionGetCookies:
		return t.doGetCookies(in)
	case ActionSetCookies:
		return t.doSetCookies(in)
	case ActionGetStorage:
		return t.doGetStorage(in)
	case ActionGetConsoleLogs:
		return t.doGetConsoleLogs(in)

	// --- Screenshot / PDF ---
	case ActionScreenshot:
		return t.doScreenshot(in)
	case ActionScreenshotElement:
		return t.doScreenshotElement(in)
	case ActionPDF:
		return t.doPDF(in)

	// --- Download ---
	case ActionSetupDownload:
		return t.doSetupDownload(in)
	case ActionListDownloads:
		return t.doListDownloads(in)

	// --- Snapshot ---
	case ActionSnapshot:
		return t.doSnapshot(in)

	// --- Scroll / HTML ---
	case ActionScroll:
		return t.doScroll(in)
	case ActionGetHTML:
		return t.doGetHTML(in)
	case ActionFindChild:
		return t.doFindChild(in)

	// --- V2: Auth/Headers ---
	case ActionSetExtraHeaders:
		return t.doSetExtraHeaders(in)
	case ActionClearExtraHeaders:
		return t.doClearExtraHeaders(in)
	case ActionSetUserAgent:
		return t.doSetUserAgent(in)
	case ActionSetHTTPAuth:
		return t.doSetHTTPAuth(in)
	case ActionInjectCookieString:
		return t.doInjectCookieString(in)
	case ActionInjectAuthToken:
		return t.doInjectAuthToken(in)

	// --- V2: Route/Block ---
	case ActionRouteAdd:
		return t.doRouteAdd(in)
	case ActionRouteRemove:
		return t.doRouteRemove(in)
	case ActionRouteList:
		return t.doRouteList(in)
	case ActionSetBlockedURLs:
		return t.doSetBlockedURLs(in)

	// --- V2: Load mode / MHTML / Blob ---
	case ActionSetLoadMode:
		return t.doSetLoadMode(in)
	case ActionSaveMHTML:
		return t.doSaveMHTML(in)
	case ActionGetBlobURL:
		return t.doGetBlobURL(in)

	// --- V3: Storage / CDP ---
	case ActionSetStorage:
		return t.doSetStorage(in)
	case ActionClearStorage:
		return t.doClearStorage(in)
	case ActionCDPSend:
		return t.doCDPSend(in)
	case ActionClearCookies:
		return t.doClearCookies(in)
	case ActionSetGeolocation:
		return t.doSetGeolocation(in)
	case ActionSetTimezone:
		return t.doSetTimezone(in)

	// --- V3: Fetch intercept ---
	case ActionFetchInterceptStart:
		return t.doFetchInterceptStart(in)
	case ActionFetchInterceptStop:
		return t.doFetchInterceptStop(in)
	case ActionNavigateWithHeaders:
		return t.doNavigateWithHeaders(in)
	case ActionExtractAuthNetwork:
		return t.doExtractAuthFromNetwork(in)

	// --- V4: HTTP dual mode ---
	case ActionCookiesToHTTP:
		return t.doCookiesToHTTP(in)
	case ActionHTTPGet:
		return t.doHTTPGet(in)
	case ActionHTTPPost:
		return t.doHTTPPost(in)
	case ActionHTTPClose:
		return t.doHTTPClose(in)
	case ActionHTTPToBrowserCookie:
		return t.doHTTPToBrowserCookies(in)
	case ActionFindElementShadow:
		return t.doFindElementShadow(in)
	case ActionClearCache:
		return t.doClearCache(in)
	case ActionGetNavHistory:
		return t.doGetNavHistory(in)

	// --- V5: CDP capabilities ---
	case ActionGetPerfMetrics:
		return t.doGetPerfMetrics(in)
	case ActionGetResponseBody:
		return t.doGetResponseBody(in)
	case ActionSetDeviceMetrics:
		return t.doSetDeviceMetrics(in)
	case ActionGetFullAXTree:
		return t.doGetFullAXTree(in)
	case ActionEnableBrowserLog:
		return t.doEnableBrowserLog(in)
	case ActionGetBrowserLogs:
		return t.doGetBrowserLogs(in)

	// --- V6: Cloudflare bypass ---
	case ActionWaitCFChallenge:
		return t.doWaitCFChallenge(in)
	case ActionExtractCFClear:
		return t.doExtractCFClearance(in)
	case ActionVerifyCFClear:
		return t.doVerifyCFClearance(in)

	// --- V7: Google bypass ---
	case ActionDetectGoogleCaptcha:
		return t.doDetectGoogleCaptcha(in)
	case ActionWaitGoogleChallenge:
		return t.doWaitGoogleChallenge(in)
	case ActionHandleGoogleConsent:
		return t.doHandleGoogleConsent(in)
	case ActionInjectGoogleCookies:
		return t.doInjectGoogleCookies(in)

	// --- Actions API ---
	case ActionMoveToAction:
		return t.doActionMoveTo(in)
	case ActionClickAtAction:
		return t.doActionClickAt(in)
	case ActionTypeAction:
		return t.doActionType(in)
	case ActionKeyDown:
		return t.doActionKeyDown(in)
	case ActionKeyUp:
		return t.doActionKeyUp(in)
	case ActionScrollAt:
		return t.doActionScrollAt(in)

	default:
		return fmt.Sprintf("Unknown action: %q. Use list_sessions, create_session, navigate, find_element, smart_click, screenshot, etc.", in.Action)
	}
}

// getSession is a helper that resolves the session from input.
func (t *BrowserTool) getSession(in *Input) (*BrowserSession, error) {
	return t.manager.GetSession(in.SessionID)
}

// getSessionAndPage resolves both session and active page.
func (t *BrowserTool) getSessionAndPage(in *Input) (*BrowserSession, *rod.Page, error) {
	s, err := t.getSession(in)
	if err != nil {
		return nil, nil, err
	}
	p := s.activePage()
	if p == nil {
		return nil, nil, fmt.Errorf("no active page in session %s", s.ID)
	}
	return s, p, nil
}

// errStr formats an error as tool output.
func errStr(err error) string {
	return fmt.Sprintf("Error: %v", err)
}

// safeInfo returns page info without panicking. Returns nil on error.
func safeInfo(page *rod.Page) *proto.TargetTargetInfo {
	info, err := page.Info()
	if err != nil {
		return &proto.TargetTargetInfo{}
	}
	return info
}
