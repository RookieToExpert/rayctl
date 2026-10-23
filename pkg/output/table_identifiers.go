package output

import "strings"

// Identifier cells may force a wider table; terminal fitting only shrinks
// descriptive cells. Field/value views apply the same rule to identity rows.
func preserveTableIdentifiers(headers []string, rows [][]string, options tableOptions) tableOptions {
	protected := map[string]struct{}{}
	for key := range options.noWrapCells {
		protected[key] = struct{}{}
	}
	minimum := make([]int, len(headers))
	copy(minimum, options.minWidths)
	fieldValue := len(headers) == 2 && (strings.EqualFold(headers[0], "FIELD") || strings.EqualFold(headers[0], "KEY"))
	for r, row := range rows {
		for c, value := range row {
			if c >= len(headers) {
				continue
			}
			identity := identifierHeader(headers[c])
			if fieldValue && c == 1 && len(row) > 0 {
				identity = identifierHeader(row[0])
			}
			_, explicit := protected[cellKey(r, c)]
			if !identity && !explicit {
				continue
			}
			protected[cellKey(r, c)] = struct{}{}
			for _, line := range strings.Split(value, "\n") {
				minimum[c] = max(minimum[c], textWidth(line))
			}
		}
	}
	options.noWrapCells = protected
	options.minWidths = minimum
	return options
}

func identifierHeader(header string) bool {
	header = strings.ToUpper(strings.TrimSpace(header))
	for _, word := range []string{"NAME", "UID", "NAMESPACE", "IMAGE", "SECRET", "HOSTNAME", "PVC", "MOUNT PATH"} {
		if strings.Contains(header, word) {
			return true
		}
	}
	switch header {
	case "POD", "PODS", "HC POD", "VC POD", "PODGROUP", "INSTANCE", "INSPECT POD", "NODE", "NODES", "HOST", "VC", "WORKSPACE", "QUEUE", "CLUSTER", "PVC", "PV", "HOST PV", "VOLUME", "CONTAINER", "SOURCE", "MOUNT", "SUBPATH", "AFS", "AID", "AIT", "ATE", "ECS", "AIS", "CREATOR", "SUBMITTER", "USER", "GROUP", "IP", "HOST IP", "POD IP", "NODE IP":
		return true
	}
	return false
}
