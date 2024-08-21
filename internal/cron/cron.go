package cron

import (
	"go.lumeweb.com/portal-plugin-billing/internal/cron/define"
	"go.lumeweb.com/portal-plugin-billing/internal/cron/tasks"
	"go.lumeweb.com/portal/core"
)

var _ core.Cronable = (*Cron)(nil)

type Cron struct {
}

func (c Cron) RegisterTasks(crn core.CronService) error {
	crn.RegisterTask(define.CronTaskUserQuotaReconcile, core.CronTaskFuncHandler[*define.CronTaskUserQuotaReconcileArgs](tasks.CronTaskUserQuotaReconcile), core.CronTaskDefinitionDaily, define.CronTaskUserQuotaReconcileArgsFactory, true)

	return nil
}

func (c Cron) ScheduleJobs(crn core.CronService) error {
	err := crn.CreateJobIfNotExists(define.CronTaskUserQuotaReconcile, nil)

	if err != nil {
		return err
	}

	return nil
}

func NewCron(_ core.Context) *Cron {
	return &Cron{}
}
