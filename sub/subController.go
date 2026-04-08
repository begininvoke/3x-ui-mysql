package sub

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v2/config"

	"github.com/gin-gonic/gin"
)

// SUBController handles HTTP requests for subscription links and JSON configurations.
type SUBController struct {
	subTitle         string
	subSupportUrl    string
	subProfileUrl    string
	subAnnounce      string
	subEnableRouting bool
	subRoutingRules  string
	subPath          string
	subJsonPath      string
	jsonEnabled      bool
	subEncrypt       bool
	updateInterval   string

	subService     *SubService
	subJsonService *SubJsonService
}

// NewSUBController creates a new subscription controller with the given configuration.
func NewSUBController(
	g *gin.RouterGroup,
	subPath string,
	jsonPath string,
	jsonEnabled bool,
	encrypt bool,
	showInfo bool,
	rModel string,
	update string,
	jsonFragment string,
	jsonNoise string,
	jsonMux string,
	jsonRules string,
	subTitle string,
	subSupportUrl string,
	subProfileUrl string,
	subAnnounce string,
	subEnableRouting bool,
	subRoutingRules string,
) *SUBController {
	sub := NewSubService(showInfo, rModel)
	a := &SUBController{
		subTitle:         subTitle,
		subSupportUrl:    subSupportUrl,
		subProfileUrl:    subProfileUrl,
		subAnnounce:      subAnnounce,
		subEnableRouting: subEnableRouting,
		subRoutingRules:  subRoutingRules,
		subPath:          subPath,
		subJsonPath:      jsonPath,
		jsonEnabled:      jsonEnabled,
		subEncrypt:       encrypt,
		updateInterval:   update,

		subService:     sub,
		subJsonService: NewSubJsonService(jsonFragment, jsonNoise, jsonMux, jsonRules, sub),
	}
	a.initRouter(g)
	return a
}

// initRouter registers HTTP routes for subscription links and JSON endpoints
// on the provided router group.
func (a *SUBController) initRouter(g *gin.RouterGroup) {
	gLink := g.Group(a.subPath)
	// Two-segment route first: /{subPath}{subid}/{host} — optional outbound host for share links.
	gLink.GET(":subid/:linkHost", a.subs)
	gLink.GET(":subid", a.subs)
	if a.jsonEnabled {
		gJson := g.Group(a.subJsonPath)
		gJson.GET(":subid/:linkHost", a.subJsons)
		gJson.GET(":subid", a.subJsons)
	}
}

// subs handles HTTP requests for subscription links, returning either HTML page or base64-encoded subscription data.
func (a *SUBController) subs(c *gin.Context) {
	subId := c.Param("subid")
	scheme, host, hostWithPort, hostHeader := a.subService.ResolveRequest(c)
	shareLinkHost, pathHasLinkHostSegment, usedQueryHost := resolveShareLinkHost(c)
	if pathHasLinkHostSegment && shareLinkHost == "" {
		c.String(400, "Error!")
		return
	}
	if shareLinkHost == "" && a.subService.SubAppendRequestHostEnabled() {
		shareLinkHost = a.subService.ResolveAutoShareLinkHost(host)
	}
	subs, lastOnline, traffic, justInfo, err := a.subService.GetSubs(subId, host, shareLinkHost)
	if err != nil || (len(subs) == 0 && !justInfo) {
		c.String(400, "Error!")
	} else {
		result := ""
		for _, sub := range subs {
			result += sub + "\n"
		}

		// If the request expects HTML (e.g., browser) or explicitly asked (?html=1 or ?view=html), render the info page here
		accept := c.GetHeader("Accept")
		if strings.Contains(strings.ToLower(accept), "text/html") || c.Query("html") == "1" || strings.EqualFold(c.Query("view"), "html") {
			// Build page data in service
			subURL, subJsonURL := a.subService.BuildURLs(scheme, hostWithPort, a.subPath, a.subJsonPath, subId)
			if shareLinkHost != "" {
				if usedQueryHost {
					subURL = a.subService.AppendHostQueryParam(subURL, shareLinkHost)
					subJsonURL = a.subService.AppendHostQueryParam(subJsonURL, shareLinkHost)
				} else {
					subURL = a.subService.AppendLinkHostSuffix(subURL, shareLinkHost)
					subJsonURL = a.subService.AppendLinkHostSuffix(subJsonURL, shareLinkHost)
				}
			}
			if !a.jsonEnabled {
				subJsonURL = ""
			}
			// Get base_path from context (set by middleware)
			basePath, exists := c.Get("base_path")
			if !exists {
				basePath = "/"
			}
			// Add subId to base_path for asset URLs
			basePathStr := basePath.(string)
			if basePathStr == "/" {
				basePathStr = "/" + subId + "/"
			} else {
				// Remove trailing slash if exists, add subId, then add trailing slash
				basePathStr = strings.TrimRight(basePathStr, "/") + "/" + subId + "/"
			}
			if pathHasLinkHostSegment && shareLinkHost != "" {
				basePathStr = basePathStr + shareLinkHost + "/"
			}
			page := a.subService.BuildPageData(subId, hostHeader, traffic, lastOnline, subs, subURL, subJsonURL, basePathStr, justInfo)
			c.HTML(200, "subpage.html", gin.H{
				"title":        "subscription.title",
				"cur_ver":      config.GetVersion(),
				"host":         page.Host,
				"base_path":    page.BasePath,
				"sId":          page.SId,
				"download":     page.Download,
				"upload":       page.Upload,
				"total":        page.Total,
				"used":         page.Used,
				"remained":     page.Remained,
				"expire":       page.Expire,
				"lastOnline":   page.LastOnline,
				"datepicker":   page.Datepicker,
				"downloadByte": page.DownloadByte,
				"uploadByte":   page.UploadByte,
				"totalByte":    page.TotalByte,
				"subUrl":       page.SubUrl,
				"subJsonUrl":   page.SubJsonUrl,
				"result":       page.Result,
				"justInfo":     page.JustInfo,
			})
			return
		}

		// Add headers
		header := fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", traffic.Up, traffic.Down, traffic.Total, traffic.ExpiryTime/1000)
		profileUrl := a.subProfileUrl
		if profileUrl == "" {
			profileUrl = fmt.Sprintf("%s://%s%s", scheme, hostWithPort, c.Request.RequestURI)
		}
		a.ApplyCommonHeaders(c, header, a.updateInterval, a.subTitle, a.subSupportUrl, profileUrl, a.subAnnounce, a.subEnableRouting, a.subRoutingRules)

		if a.subEncrypt {
			c.String(200, base64.StdEncoding.EncodeToString([]byte(result)))
		} else {
			c.String(200, result)
		}
	}
}

// subJsons handles HTTP requests for JSON subscription configurations.
func (a *SUBController) subJsons(c *gin.Context) {
	subId := c.Param("subid")
	scheme, host, hostWithPort, _ := a.subService.ResolveRequest(c)
	shareLinkHost, pathHasLinkHostSegment, _ := resolveShareLinkHost(c)
	if pathHasLinkHostSegment && shareLinkHost == "" {
		c.String(400, "Error!")
		return
	}
	if shareLinkHost == "" && a.subService.SubAppendRequestHostEnabled() {
		shareLinkHost = a.subService.ResolveAutoShareLinkHost(host)
	}
	jsonDest := host
	if shareLinkHost != "" {
		jsonDest = shareLinkHost
	}
	jsonSub, header, err := a.subJsonService.GetJson(subId, jsonDest)
	if err != nil || len(jsonSub) == 0 {
		c.String(400, "Error!")
	} else {
		// Add headers
		profileUrl := a.subProfileUrl
		if profileUrl == "" {
			profileUrl = fmt.Sprintf("%s://%s%s", scheme, hostWithPort, c.Request.RequestURI)
		}
		a.ApplyCommonHeaders(c, header, a.updateInterval, a.subTitle, a.subSupportUrl, profileUrl, a.subAnnounce, a.subEnableRouting, a.subRoutingRules)

		c.String(200, jsonSub)
	}
}

// ApplyCommonHeaders sets common HTTP headers for subscription responses including user info, update interval, and profile title.
func (a *SUBController) ApplyCommonHeaders(
	c *gin.Context,
	header,
	updateInterval,
	profileTitle string,
	profileSupportUrl string,
	profileUrl string,
	profileAnnounce string,
	profileEnableRouting bool,
	profileRoutingRules string,
) {
	c.Writer.Header().Set("Subscription-Userinfo", header)
	c.Writer.Header().Set("Profile-Update-Interval", updateInterval)

	//Basics
	if profileTitle != "" {
		c.Writer.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte(profileTitle)))
	}
	if profileSupportUrl != "" {
		c.Writer.Header().Set("Support-Url", profileSupportUrl)
	}
	if profileUrl != "" {
		c.Writer.Header().Set("Profile-Web-Page-Url", profileUrl)
	}
	if profileAnnounce != "" {
		c.Writer.Header().Set("Announce", "base64:"+base64.StdEncoding.EncodeToString([]byte(profileAnnounce)))
	}

	//Advanced (Happ)
	c.Writer.Header().Set("Routing-Enable", strconv.FormatBool(profileEnableRouting))
	if profileRoutingRules != "" {
		c.Writer.Header().Set("Routing", profileRoutingRules)
	}
}

// resolveShareLinkHost parses explicit outbound host from path …/subid/{host} or ?host= (legacy ?linkHost=).
// usedQuery is true when host came from query (display keeps ?host= style).
func resolveShareLinkHost(c *gin.Context) (shareHost string, pathHasLinkHostSegment bool, usedQuery bool) {
	rawPath := strings.TrimSpace(c.Param("linkHost"))
	if rawPath != "" {
		pathHasLinkHostSegment = true
		shareHost = SanitizeSubscriptionLinkHost(rawPath)
		return shareHost, pathHasLinkHostSegment, false
	}
	if h := strings.TrimSpace(c.Query("host")); h != "" {
		shareHost = SanitizeSubscriptionLinkHost(h)
		return shareHost, false, true
	}
	if h := strings.TrimSpace(c.Query("linkHost")); h != "" {
		shareHost = SanitizeSubscriptionLinkHost(h)
		return shareHost, false, true
	}
	return "", false, false
}
