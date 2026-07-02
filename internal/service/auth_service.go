package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"rayctl/internal/platform"
)

type AuthService struct {
	vcClient *platform.VirtualClusterClient
}

type AuthAFSResult struct {
	AFSName string
	Items   []AuthAFSItem
}

type AuthAFSItem struct {
	MemberType     string
	MemberName     string
	MemberIdentify string
	MemberValue    string
	Roles          string
	RoleNames      string
	PolicyID       string
	CreateTime     string
}

func NewAuthService(vcClient *platform.VirtualClusterClient) *AuthService {
	return &AuthService{vcClient: vcClient}
}

func (s *AuthService) GetAFS(ctx context.Context, afsName string) (*AuthAFSResult, error) {
	if s == nil || s.vcClient == nil {
		return nil, fmt.Errorf("platform client is required for auth lookup")
	}
	afsName = strings.TrimSpace(afsName)
	if afsName == "" {
		return nil, fmt.Errorf("afs name is required")
	}

	policies, err := s.vcClient.ListIAMBindingPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list binding policies: %w", err)
	}

	items := make([]AuthAFSItem, 0)
	for _, policy := range policies {
		if extractAFSNameFromScope(policy.Scope) != afsName {
			continue
		}
		roleDisplays, roleNames := extractIAMRoleNames(policy.RoleInfos)
		items = append(items, AuthAFSItem{
			MemberType:     strings.TrimSpace(policy.MemberType),
			MemberName:     strings.TrimSpace(policy.MemberName),
			MemberIdentify: strings.TrimSpace(policy.MemberIdentify),
			MemberValue:    strings.TrimSpace(policy.MemberValue),
			Roles:          strings.Join(roleDisplays, ","),
			RoleNames:      strings.Join(roleNames, ","),
			PolicyID:       strings.TrimSpace(policy.ID),
			CreateTime:     formatLocalTime(policy.CreateTime),
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].MemberType != items[j].MemberType {
			return items[i].MemberType < items[j].MemberType
		}
		if items[i].MemberIdentify != items[j].MemberIdentify {
			return items[i].MemberIdentify < items[j].MemberIdentify
		}
		if items[i].Roles != items[j].Roles {
			return items[i].Roles < items[j].Roles
		}
		return items[i].PolicyID < items[j].PolicyID
	})

	return &AuthAFSResult{
		AFSName: afsName,
		Items:   items,
	}, nil
}

func extractAFSNameFromScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return ""
	}
	marker := "/virtualVolumes/"
	index := strings.Index(scope, marker)
	if index < 0 {
		return ""
	}
	name := scope[index+len(marker):]
	if cut := strings.IndexAny(name, "/?#"); cut >= 0 {
		name = name[:cut]
	}
	return strings.TrimSpace(name)
}

func extractIAMRoleNames(roles []platform.IAMRoleInfo) ([]string, []string) {
	displays := make([]string, 0, len(roles))
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		if display := strings.TrimSpace(role.DisplayName); display != "" {
			displays = append(displays, display)
		}
		if name := strings.TrimSpace(role.RoleName); name != "" {
			names = append(names, name)
		}
	}
	return displays, names
}

func formatLocalTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return t.In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
}
