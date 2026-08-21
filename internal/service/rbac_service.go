package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"rayctl/internal/platform"
)

const defaultRBACLabelSelector = "resource.compute.sensecore.cn/control"

var allowedRBACGrantRoles = map[string]struct{}{
	"cluster-admin": {},
	"admin":         {},
	"edit":          {},
	"view":          {},
}

type RBACService struct {
	vcClient *platform.VirtualClusterClient
}

type RBACGetResult struct {
	ClusterName   string
	ClusterUID    string
	ClusterRef    string
	ProfileName   string
	LabelSelector string
	Items         []RBACBindingItem
}

type RBACGetRequest struct {
	ClusterIdentifier string
	Environment       string
	LabelSelector     string
	BearerToken       string
	ProfileTokens     map[string]string
}

type RBACBindingItem struct {
	Kind      string
	Namespace string
	Name      string
	Role      string
	Subjects  string
	CreatedAt string
}

type RBACGrantRequest struct {
	ClusterIdentifier  string
	Namespace          string
	Role               string
	Users              []string
	Groups             []string
	IAMBearerToken     string
	ComputeBearerToken string
	DryRun             bool
}

type RBACGrantMember struct {
	Type        string
	Name        string
	DisplayName string
	ID          string
	Status      string
}

type RBACGrantResult struct {
	ClusterName  string
	ClusterUID   string
	ClusterRef   string
	ProfileName  string
	Namespace    string
	BindingKind  string
	BindingName  string
	Role         string
	AccessReason string
	Members      []RBACGrantMember
	Payload      string
	Result       string

	clusterBinding *rbacv1.ClusterRoleBinding
	roleBinding    *rbacv1.RoleBinding
}

type RBACRemoveRequest struct {
	ClusterIdentifier  string
	Namespace          string
	BindingName        string
	ComputeBearerToken string
	DryRun             bool
}

type RBACRemoveResult struct {
	ClusterName  string
	ClusterUID   string
	ClusterRef   string
	ProfileName  string
	Namespace    string
	BindingKind  string
	BindingName  string
	Role         string
	Subjects     string
	SubjectCount int
	AccessReason string
	Result       string

	clusterWide bool
}

func NewRBACService(vcClient *platform.VirtualClusterClient) *RBACService {
	return &RBACService{vcClient: vcClient}
}

func (s *RBACService) PrepareRemove(ctx context.Context, req RBACRemoveRequest) (*RBACRemoveResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for rbac remove")
	}
	if strings.TrimSpace(req.ComputeBearerToken) == "" {
		return nil, fmt.Errorf("rbac remove requires compute id_token; run rayctl auth login first")
	}

	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required; use --namespace all for cluster-wide access")
	}
	bindingName := strings.TrimSpace(req.BindingName)
	if bindingName == "" {
		return nil, fmt.Errorf("binding name is required")
	}

	clusterName, clusterUID, profileName, err := s.resolveCluster(ctx, req.ClusterIdentifier)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(profileName) == "" {
		return nil, fmt.Errorf("cannot determine platform profile for virtual cluster %q", req.ClusterIdentifier)
	}
	clusterRef := "vc-" + clusterUID
	clusterWide := strings.EqualFold(namespace, "all")
	bindingKind := "RoleBinding"
	resource := "rolebindings"
	reviewNamespace := namespace
	if clusterWide {
		namespace = "all"
		bindingKind = "ClusterRoleBinding"
		resource = "clusterrolebindings"
		reviewNamespace = ""
	}

	review, err := s.vcClient.ReviewRBACAccessForProfileToken(ctx, profileName, clusterRef, reviewNamespace, "delete", resource, req.ComputeBearerToken)
	if err != nil {
		return nil, fmt.Errorf("check permission to delete %s: %w", resource, err)
	}
	if !review.Status.Allowed {
		reason := firstNonEmpty(strings.TrimSpace(review.Status.Reason), strings.TrimSpace(review.Status.EvaluationError), "access denied")
		return nil, fmt.Errorf("current console user cannot delete %s in VC %s: %s", bindingKind, clusterName, reason)
	}

	result := &RBACRemoveResult{
		ClusterName:  clusterName,
		ClusterUID:   clusterUID,
		ClusterRef:   clusterRef,
		ProfileName:  profileName,
		Namespace:    namespace,
		BindingKind:  bindingKind,
		BindingName:  bindingName,
		AccessReason: firstNonEmpty(strings.TrimSpace(review.Status.Reason), "allowed"),
		Result:       "pending confirmation",
		clusterWide:  clusterWide,
	}
	if clusterWide {
		bindings, err := s.vcClient.ListClusterRoleBindingsForProfileToken(ctx, profileName, clusterRef, defaultRBACLabelSelector, req.ComputeBearerToken)
		if err != nil {
			return nil, fmt.Errorf("list clusterrolebindings: %w", err)
		}
		binding, ok := findClusterRoleBindingByName(bindings, bindingName)
		if !ok {
			return nil, fmt.Errorf("controlled ClusterRoleBinding %q not found in VC %s", bindingName, clusterName)
		}
		result.Role = roleRefText(binding.RoleRef)
		result.Subjects = subjectsText(binding.Subjects, binding.Annotations)
		result.SubjectCount = len(binding.Subjects)
	} else {
		bindings, err := s.vcClient.ListRoleBindingsForProfileToken(ctx, profileName, clusterRef, namespace, defaultRBACLabelSelector, req.ComputeBearerToken)
		if err != nil {
			return nil, fmt.Errorf("list rolebindings: %w", err)
		}
		binding, ok := findRoleBindingByName(bindings, bindingName)
		if !ok {
			return nil, fmt.Errorf("controlled RoleBinding %q not found in namespace %s of VC %s", bindingName, namespace, clusterName)
		}
		result.Role = roleRefText(binding.RoleRef)
		result.Subjects = subjectsText(binding.Subjects, binding.Annotations)
		result.SubjectCount = len(binding.Subjects)
	}
	if req.DryRun {
		result.Result = "dry-run"
	}
	return result, nil
}

func (s *RBACService) ApplyRemove(ctx context.Context, result *RBACRemoveResult, bearerToken string) error {
	if result == nil {
		return fmt.Errorf("prepared rbac remove is required")
	}
	if strings.TrimSpace(bearerToken) == "" {
		return fmt.Errorf("compute bearer token is required")
	}
	var err error
	if result.clusterWide {
		err = s.vcClient.DeleteClusterRoleBindingForProfileToken(ctx, result.ProfileName, result.ClusterRef, result.BindingName, bearerToken)
	} else {
		err = s.vcClient.DeleteRoleBindingForProfileToken(ctx, result.ProfileName, result.ClusterRef, result.Namespace, result.BindingName, bearerToken)
	}
	if err != nil {
		return fmt.Errorf("delete %s %s: %w", result.BindingKind, result.BindingName, err)
	}
	result.Result = "deleted"
	return nil
}

func (s *RBACService) PrepareGrant(ctx context.Context, req RBACGrantRequest) (*RBACGrantResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for rbac grant")
	}
	if strings.TrimSpace(req.IAMBearerToken) == "" || strings.TrimSpace(req.ComputeBearerToken) == "" {
		return nil, fmt.Errorf("rbac grant requires IAM access_token and compute id_token; run rayctl auth login first")
	}

	namespace := strings.TrimSpace(req.Namespace)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required; use --namespace all for cluster-wide access")
	}
	role := strings.ToLower(strings.TrimSpace(req.Role))
	if _, ok := allowedRBACGrantRoles[role]; !ok {
		return nil, fmt.Errorf("unsupported role %q; allowed roles: cluster-admin, admin, edit, view", req.Role)
	}
	if len(req.Users) == 0 && len(req.Groups) == 0 {
		return nil, fmt.Errorf("at least one --user or --group is required")
	}

	clusterName, clusterUID, profileName, err := s.resolveCluster(ctx, req.ClusterIdentifier)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(profileName) == "" {
		return nil, fmt.Errorf("cannot determine platform profile for virtual cluster %q", req.ClusterIdentifier)
	}
	clusterRef := "vc-" + clusterUID
	clusterWide := strings.EqualFold(namespace, "all")
	bindingKind := "RoleBinding"
	resource := "rolebindings"
	reviewNamespace := namespace
	if clusterWide {
		namespace = "all"
		bindingKind = "ClusterRoleBinding"
		resource = "clusterrolebindings"
		reviewNamespace = ""
	}

	review, err := s.vcClient.ReviewRBACCreateAccessForProfileToken(ctx, profileName, clusterRef, reviewNamespace, resource, req.ComputeBearerToken)
	if err != nil {
		return nil, fmt.Errorf("check permission to create %s: %w", resource, err)
	}
	if !review.Status.Allowed {
		reason := firstNonEmpty(strings.TrimSpace(review.Status.Reason), strings.TrimSpace(review.Status.EvaluationError), "access denied")
		return nil, fmt.Errorf("current console user cannot create %s in VC %s: %s", bindingKind, clusterName, reason)
	}

	members, err := s.resolveGrantMembers(ctx, profileName, req.Users, req.Groups, req.IAMBearerToken)
	if err != nil {
		return nil, err
	}
	existingSubjects, err := s.existingRoleSubjects(ctx, profileName, clusterRef, namespace, clusterWide, role, req.ComputeBearerToken)
	if err != nil {
		return nil, err
	}

	pending := make([]RBACGrantMember, 0, len(members))
	for i := range members {
		key := strings.ToLower(members[i].Type) + "/" + members[i].ID
		if _, ok := existingSubjects[key]; ok {
			members[i].Status = "already granted"
			continue
		}
		members[i].Status = "pending"
		pending = append(pending, members[i])
	}

	result := &RBACGrantResult{
		ClusterName:  clusterName,
		ClusterUID:   clusterUID,
		ClusterRef:   clusterRef,
		ProfileName:  profileName,
		Namespace:    namespace,
		BindingKind:  bindingKind,
		Role:         role,
		AccessReason: firstNonEmpty(strings.TrimSpace(review.Status.Reason), "allowed"),
		Members:      members,
		Result:       "pending confirmation",
	}
	if len(pending) == 0 {
		result.Result = "already granted"
		return result, nil
	}

	annotations, subjects := grantBindingMembers(pending, namespace, !clusterWide)
	if clusterWide {
		binding := &rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRoleBinding"},
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: role + "-",
				Labels:       map[string]string{"resource.compute.sensecore.cn/control": "true"},
				Annotations:  annotations,
			},
			RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: role},
			Subjects: subjects,
		}
		result.clusterBinding = binding
		result.Payload = marshalRBACPayload(binding)
	} else {
		binding := &rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "RoleBinding"},
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: role + "-",
				Namespace:    namespace,
				Labels:       map[string]string{"resource.compute.sensecore.cn/control": "true"},
				Annotations:  annotations,
			},
			RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: role},
			Subjects: subjects,
		}
		result.roleBinding = binding
		result.Payload = marshalRBACPayload(binding)
	}
	if req.DryRun {
		result.Result = "dry-run"
	}
	return result, nil
}

func (s *RBACService) ApplyGrant(ctx context.Context, result *RBACGrantResult, bearerToken string) error {
	if result == nil {
		return fmt.Errorf("prepared rbac grant is required")
	}
	if strings.TrimSpace(bearerToken) == "" {
		return fmt.Errorf("console bearer token is required")
	}
	if result.Result == "already granted" {
		return nil
	}

	switch {
	case result.clusterBinding != nil:
		binding, err := s.vcClient.CreateClusterRoleBindingForProfileToken(ctx, result.ProfileName, result.ClusterRef, *result.clusterBinding, bearerToken)
		if err != nil {
			return fmt.Errorf("create clusterrolebinding: %w", err)
		}
		result.BindingName = binding.Name
	case result.roleBinding != nil:
		binding, err := s.vcClient.CreateRoleBindingForProfileToken(ctx, result.ProfileName, result.ClusterRef, result.Namespace, *result.roleBinding, bearerToken)
		if err != nil {
			return fmt.Errorf("create rolebinding: %w", err)
		}
		result.BindingName = binding.Name
	default:
		return fmt.Errorf("prepared rbac binding payload is missing")
	}
	for i := range result.Members {
		if result.Members[i].Status == "pending" {
			result.Members[i].Status = "created"
		}
	}
	result.Result = "created"
	return nil
}

func (s *RBACService) resolveGrantMembers(ctx context.Context, profileName string, users []string, groups []string, bearerToken string) ([]RBACGrantMember, error) {
	type requestedMember struct {
		kind       string
		identifier string
	}
	requested := make([]requestedMember, 0, len(users)+len(groups))
	for _, user := range users {
		if value := strings.TrimSpace(user); value != "" {
			requested = append(requested, requestedMember{kind: "User", identifier: value})
		}
	}
	for _, group := range groups {
		if value := strings.TrimSpace(group); value != "" {
			requested = append(requested, requestedMember{kind: "Group", identifier: value})
		}
	}
	if len(requested) == 0 {
		return nil, fmt.Errorf("at least one non-empty --user or --group is required")
	}

	result := make([]RBACGrantMember, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, target := range requested {
		items, err := s.vcClient.SearchIAMUserGroupsForProfileToken(ctx, profileName, target.identifier, bearerToken)
		if err != nil {
			return nil, fmt.Errorf("search %s %q: %w", strings.ToLower(target.kind), target.identifier, err)
		}
		matches := make([]platform.IAMUserGroupSearchItem, 0)
		for _, item := range items {
			if !rbacMemberTypeMatches(item.Type, target.kind) {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(item.ID), target.identifier) || strings.EqualFold(strings.TrimSpace(item.Name), target.identifier) {
				matches = append(matches, item)
			}
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("%s %q not found; use an exact name or ID", strings.ToLower(target.kind), target.identifier)
		}
		if len(matches) > 1 {
			candidates := make([]string, 0, len(matches))
			for _, item := range matches {
				candidates = append(candidates, fmt.Sprintf("%s(%s)", item.Name, item.ID))
			}
			sort.Strings(candidates)
			return nil, fmt.Errorf("%s %q matched multiple members: %s", strings.ToLower(target.kind), target.identifier, strings.Join(candidates, ", "))
		}
		item := matches[0]
		key := strings.ToLower(target.kind) + "/" + strings.TrimSpace(item.ID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, RBACGrantMember{
			Type:        target.kind,
			Name:        strings.TrimSpace(item.Name),
			DisplayName: strings.TrimSpace(item.DisplayName),
			ID:          strings.TrimSpace(item.ID),
		})
	}
	return result, nil
}

func (s *RBACService) existingRoleSubjects(ctx context.Context, profileName string, clusterRef string, namespace string, clusterWide bool, role string, bearerToken string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	if clusterWide {
		bindings, err := s.vcClient.ListClusterRoleBindingsForProfileToken(ctx, profileName, clusterRef, defaultRBACLabelSelector, bearerToken)
		if err != nil {
			return nil, fmt.Errorf("list existing clusterrolebindings: %w", err)
		}
		for _, binding := range bindings {
			if !rbacRoleRefMatches(binding.RoleRef, role) {
				continue
			}
			addRBACSubjects(result, binding.Subjects)
		}
		return result, nil
	}

	bindings, err := s.vcClient.ListRoleBindingsForProfileToken(ctx, profileName, clusterRef, namespace, defaultRBACLabelSelector, bearerToken)
	if err != nil {
		return nil, fmt.Errorf("list existing rolebindings in namespace %q: %w", namespace, err)
	}
	for _, binding := range bindings {
		if !rbacRoleRefMatches(binding.RoleRef, role) {
			continue
		}
		addRBACSubjects(result, binding.Subjects)
	}
	return result, nil
}

func rbacMemberTypeMatches(actual string, expected string) bool {
	actual = strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.TrimSpace(actual)))
	switch strings.ToLower(strings.TrimSpace(expected)) {
	case "user":
		return actual == "user"
	case "group":
		return actual == "group" || actual == "usergroup"
	default:
		return false
	}
}

func rbacRoleRefMatches(roleRef rbacv1.RoleRef, role string) bool {
	return strings.EqualFold(strings.TrimSpace(roleRef.Kind), "ClusterRole") && strings.EqualFold(strings.TrimSpace(roleRef.Name), strings.TrimSpace(role))
}

func addRBACSubjects(target map[string]struct{}, subjects []rbacv1.Subject) {
	for _, subject := range subjects {
		kind := strings.ToLower(strings.TrimSpace(subject.Kind))
		name := strings.TrimSpace(subject.Name)
		if (kind != "user" && kind != "group") || name == "" {
			continue
		}
		target[kind+"/"+name] = struct{}{}
	}
}

func findClusterRoleBindingByName(bindings []rbacv1.ClusterRoleBinding, name string) (rbacv1.ClusterRoleBinding, bool) {
	name = strings.TrimSpace(name)
	for _, binding := range bindings {
		if strings.TrimSpace(binding.Name) == name {
			return binding, true
		}
	}
	return rbacv1.ClusterRoleBinding{}, false
}

func findRoleBindingByName(bindings []rbacv1.RoleBinding, name string) (rbacv1.RoleBinding, bool) {
	name = strings.TrimSpace(name)
	for _, binding := range bindings {
		if strings.TrimSpace(binding.Name) == name {
			return binding, true
		}
	}
	return rbacv1.RoleBinding{}, false
}

func grantBindingMembers(members []RBACGrantMember, namespace string, namespaced bool) (map[string]string, []rbacv1.Subject) {
	annotations := make(map[string]string, len(members))
	subjects := make([]rbacv1.Subject, 0, len(members))
	for _, member := range members {
		kind := "User"
		prefix := "user"
		if strings.EqualFold(member.Type, "Group") {
			kind = "Group"
			prefix = "group"
		}
		annotations[prefix+"/"+member.ID] = member.Name
		subject := rbacv1.Subject{Kind: kind, Name: member.ID}
		if namespaced {
			subject.Namespace = namespace
		}
		subjects = append(subjects, subject)
	}
	return annotations, subjects
}

func marshalRBACPayload(value any) string {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return ""
	}
	return string(payload)
}

func (s *RBACService) Get(ctx context.Context, clusterIdentifier string, labelSelector string, bearerToken string) (*RBACGetResult, error) {
	return s.GetWithOptions(ctx, RBACGetRequest{
		ClusterIdentifier: clusterIdentifier,
		LabelSelector:     labelSelector,
		BearerToken:       bearerToken,
	})
}

func (s *RBACService) GetWithOptions(ctx context.Context, req RBACGetRequest) (*RBACGetResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for rbac lookup")
	}
	clusterIdentifier := strings.TrimSpace(req.ClusterIdentifier)
	if clusterIdentifier == "" {
		return nil, fmt.Errorf("cluster identifier is required")
	}
	labelSelector := strings.TrimSpace(req.LabelSelector)
	if labelSelector == "" {
		labelSelector = defaultRBACLabelSelector
	}

	clusterName, clusterUID, profileName, err := s.resolveClusterForEnvironment(ctx, clusterIdentifier, req.Environment)
	if err != nil {
		return nil, err
	}
	bearerToken := strings.TrimSpace(req.ProfileTokens[profileName])
	if bearerToken == "" {
		bearerToken = strings.TrimSpace(req.BearerToken)
	}
	if bearerToken == "" {
		environment := s.vcClient.ProfileEnvironment(profileName)
		return nil, fmt.Errorf(
			"virtual cluster %q resolved to profile %q (%s), but no valid console id_token exists for that profile; run rayctl auth login -e %s, or provide RAYCTL_BEARER_TOKEN",
			clusterName,
			profileName,
			firstNonEmpty(environment, "unknown"),
			firstNonEmpty(environment, profileName),
		)
	}
	clusterRef := "vc-" + clusterUID

	bindings, err := s.vcClient.ListClusterRoleBindingsForProfileToken(ctx, profileName, clusterRef, labelSelector, bearerToken)
	if err != nil {
		return nil, fmt.Errorf("list clusterrolebindings: %w", err)
	}
	roleBindings, err := s.vcClient.ListAllRoleBindingsForProfileToken(ctx, profileName, clusterRef, labelSelector, bearerToken)
	if err != nil {
		return nil, fmt.Errorf("list rolebindings: %w", err)
	}

	items := make([]RBACBindingItem, 0, len(bindings)+len(roleBindings))
	for _, binding := range bindings {
		items = append(items, RBACBindingItem{
			Kind:      "CRB",
			Namespace: "all",
			Name:      binding.Name,
			Role:      roleRefText(binding.RoleRef),
			Subjects:  subjectsText(binding.Subjects, binding.Annotations),
			CreatedAt: formatRBACLocalTime(binding),
		})
	}
	for _, binding := range roleBindings {
		items = append(items, RBACBindingItem{
			Kind:      "RB",
			Namespace: firstNonEmpty(strings.TrimSpace(binding.Namespace), "-"),
			Name:      binding.Name,
			Role:      roleRefText(binding.RoleRef),
			Subjects:  subjectsText(binding.Subjects, binding.Annotations),
			CreatedAt: formatRBACRoleBindingLocalTime(binding),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		if items[i].Role != items[j].Role {
			return items[i].Role < items[j].Role
		}
		return items[i].Name < items[j].Name
	})

	return &RBACGetResult{
		ClusterName:   clusterName,
		ClusterUID:    clusterUID,
		ClusterRef:    clusterRef,
		ProfileName:   profileName,
		LabelSelector: labelSelector,
		Items:         items,
	}, nil
}

func (s *RBACService) resolveCluster(ctx context.Context, identifier string) (string, string, string, error) {
	return s.resolveClusterForEnvironment(ctx, identifier, "")
}

func (s *RBACService) resolveClusterForEnvironment(ctx context.Context, identifier string, environment string) (string, string, string, error) {
	clusters, err := s.vcClient.ListVirtualClustersForEnvironment(ctx, environment)
	if err != nil {
		return "", "", "", fmt.Errorf("list virtual clusters: %w", err)
	}

	normalized := strings.ToLower(strings.TrimSpace(identifier))
	exact := make([]platform.VirtualCluster, 0)
	fuzzy := make([]platform.VirtualCluster, 0)
	for _, cluster := range clusters {
		fields := []string{
			strings.TrimSpace(cluster.Name),
			strings.TrimSpace(cluster.DisplayName),
			strings.TrimSpace(cluster.UID),
			"vc-" + strings.TrimSpace(cluster.UID),
		}
		matchedExact := false
		matchedFuzzy := false
		for _, field := range fields {
			field = strings.ToLower(strings.TrimSpace(field))
			if field == "" {
				continue
			}
			if field == normalized {
				matchedExact = true
				break
			}
			if strings.Contains(field, normalized) {
				matchedFuzzy = true
			}
		}
		if matchedExact {
			exact = append(exact, cluster)
			continue
		}
		if matchedFuzzy {
			fuzzy = append(fuzzy, cluster)
		}
	}

	switch {
	case len(exact) == 1:
		return firstNonEmpty(exact[0].Name, exact[0].DisplayName, "vc-"+exact[0].UID), exact[0].UID, exact[0].ProfileName, nil
	case len(exact) > 1:
		return "", "", "", fmt.Errorf("cluster %q matched multiple virtual clusters: %s; use -e d, -e pt, or -e dcloud to select an environment", identifier, s.rbacClusterCandidates(exact))
	case len(fuzzy) == 1:
		return firstNonEmpty(fuzzy[0].Name, fuzzy[0].DisplayName, "vc-"+fuzzy[0].UID), fuzzy[0].UID, fuzzy[0].ProfileName, nil
	case len(fuzzy) > 1:
		return "", "", "", fmt.Errorf("cluster %q matched multiple virtual clusters: %s; use -e d, -e pt, or -e dcloud to select an environment", identifier, s.rbacClusterCandidates(fuzzy))
	default:
		if name, uid, ok := rawRBACClusterReference(identifier); ok {
			return name, uid, "", nil
		}
		return "", "", "", fmt.Errorf("virtual cluster %q not found", identifier)
	}
}

func rawRBACClusterReference(identifier string) (string, string, bool) {
	identifier = strings.TrimSpace(identifier)
	uid := strings.TrimPrefix(identifier, "vc-")
	if looksLikeUUID(uid) {
		return "vc-" + uid, uid, true
	}
	if looksLikeUUID(identifier) {
		return "vc-" + identifier, identifier, true
	}
	return "", "", false
}

func (s *RBACService) rbacClusterCandidates(items []platform.VirtualCluster) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		name := firstNonEmpty(item.Name, item.DisplayName, "vc-"+item.UID)
		environment := s.vcClient.ProfileEnvironment(item.ProfileName)
		parts = append(parts, fmt.Sprintf("%s [%s/%s]", name, firstNonEmpty(environment, "unknown"), item.ProfileName))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func roleRefText(role rbacv1.RoleRef) string {
	return strings.TrimSpace(role.Name)
}

func formatRBACLocalTime(binding rbacv1.ClusterRoleBinding) string {
	if binding.CreationTimestamp.IsZero() {
		return ""
	}
	return formatLocalTime(binding.CreationTimestamp.Time.Format("2006-01-02T15:04:05.999999999Z07:00"))
}

func formatRBACRoleBindingLocalTime(binding rbacv1.RoleBinding) string {
	if binding.CreationTimestamp.IsZero() {
		return ""
	}
	return formatLocalTime(binding.CreationTimestamp.Time.Format("2006-01-02T15:04:05.999999999Z07:00"))
}

func subjectsText(subjects []rbacv1.Subject, annotations map[string]string) string {
	if len(subjects) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		kind := strings.ToLower(strings.TrimSpace(subject.Kind))
		name := strings.TrimSpace(subject.Name)
		value := strings.TrimSpace(annotations[kind+"/"+name])
		if value == "" {
			value = name
		}
		if value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	sort.Strings(parts)
	if len(parts) == 1 {
		return parts[0]
	}
	lines := make([]string, 0, len(parts))
	for index, part := range parts {
		lines = append(lines, fmt.Sprintf("%d. %s", index+1, part))
	}
	return strings.Join(lines, "\n")
}
