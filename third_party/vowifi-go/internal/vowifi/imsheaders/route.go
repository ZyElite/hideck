package imsheaders

import "strings"

// RouteSet returns the non-empty service route and outbound proxy in order.
func RouteSet(serviceRoute, outboundProxy string) []string {
	routes := make([]string, 0, 2)
	if route := strings.TrimSpace(serviceRoute); route != "" {
		routes = append(routes, route)
	}
	if route := strings.TrimSpace(outboundProxy); route != "" {
		routes = append(routes, route)
	}
	return routes
}

// FirstRoute returns the first effective route.
func FirstRoute(serviceRoute, outboundProxy string) string {
	routes := RouteSet(serviceRoute, outboundProxy)
	if len(routes) == 0 {
		return ""
	}
	return routes[0]
}
