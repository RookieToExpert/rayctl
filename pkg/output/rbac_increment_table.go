package output

import "rayctl/internal/service"

func printRBACIncrement(r *service.RBACGetResult) {
	printDetailAnchor(r.ClusterName, "", "", "", r.ProfileName)
	rows := [][]string{}
	for _, item := range r.Items {
		rows = append(rows, []string{item.Namespace, item.Kind, item.CreatedAt})
	}
	if len(rows) > 0 {
		printBoxTable([]string{"NAMESPACE", "TYPE", "CREATE TIME"}, rows)
	}
}
