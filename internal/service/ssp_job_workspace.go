package service

func (r *SSPJobGetResult) IsQueryComplete() bool { return r == nil || !r.QueryIncomplete }
