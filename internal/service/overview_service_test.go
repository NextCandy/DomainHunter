package service

import (
	"context"
	"testing"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/repository"
)

type overviewTrendRepoStub struct {
	repository.ObservationRepository
	counts []repository.DailyStatusCount
}

func (r *overviewTrendRepoStub) DailyStatusCounts(context.Context, string, string) ([]repository.DailyStatusCount, error) {
	return append([]repository.DailyStatusCount(nil), r.counts...), nil
}

func (r *overviewTrendRepoStub) ChangesBetween(context.Context, string, string) ([]repository.ObservationChange, error) {
	return nil, nil
}

func TestOverviewTrendReturnsStableZeroFilledContract(t *testing.T) {
	today := time.Now().In(time.Local).Format(trendDayLayout)
	repo := &overviewTrendRepoStub{counts: []repository.DailyStatusCount{
		{Day: today, Status: domain.StatusRegistered, Count: 2, Changed: 1, HighConfidence: 2},
		{Day: today, Status: domain.StatusAvailable, Count: 1, Changed: 1, HighConfidence: 1},
	}}
	service := &OverviewService{observations: repo}

	trend, err := service.Trend(context.Background(), 3)
	if err != nil {
		t.Fatalf("生成趋势失败: %v", err)
	}
	if trend.Days != 3 || len(trend.Points) != 3 {
		t.Fatalf("趋势天数契约错误: %+v", trend)
	}
	last := trend.Points[len(trend.Points)-1]
	if last.Day != today || last.Total != 3 || last.Available != 1 || last.Changes != 2 || last.HighScore != 3 {
		t.Fatalf("当天派生计数错误: %+v", last)
	}
	if last.StatusCounts[domain.StatusRegistered] != 2 || last.StatusCounts[domain.StatusAvailable] != 1 {
		t.Fatalf("逐状态计数错误: %+v", last.StatusCounts)
	}
	for _, status := range domain.AllStatuses() {
		if _, ok := last.StatusCounts[status]; !ok {
			t.Fatalf("稳定契约缺少状态键 %q", status)
		}
	}
	if trend.Points[0].Total != 0 || trend.Points[1].Total != 0 {
		t.Fatalf("无记录日期应返回零值: %+v", trend.Points)
	}
}

func TestOverviewTrendUsesDefaultDaysWhenUnset(t *testing.T) {
	repo := &overviewTrendRepoStub{}
	service := &OverviewService{observations: repo}

	trend, err := service.Trend(context.Background(), 0)
	if err != nil {
		t.Fatalf("默认趋势失败: %v", err)
	}
	if trend.Days != defaultTrendDays || len(trend.Points) != defaultTrendDays {
		t.Fatalf("days 缺省应为 %d: %+v", defaultTrendDays, trend)
	}
}
