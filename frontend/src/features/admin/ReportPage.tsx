import { Download } from 'lucide-react'
import { useSearchParams } from 'react-router'
import { useCenter } from '@/app/center'
import { MonthSwitcher } from '@/components/shared'
import { Button, Card, CardHeader, ErrorState, MoneyText, PageHeader, SkeletonList, layout, useToast } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { formatDate } from '@/utils/date'
import { errorMessage } from '@/utils/errors'
import { useMonthReport, useReportExport } from './api'
import s from './Report.module.css'

const MONTH = /^\d{4}-(0[1-9]|1[0-2])$/

export function ReportPage() {
  const c = useCenter()
  const [params, setParams] = useSearchParams()
  const now = c.today.slice(0, 7) // текущий месяц в поясе центра
  const raw = params.get('month') ?? ''
  const month = MONTH.test(raw) && raw <= now ? raw : now
  const report = useMonthReport(c.id, month)
  const exp = useReportExport(c.id)
  const toast = useToast()
  const r = report.data

  return (
    <>
      <PageHeader
        title={ru.report.title}
        subtitle={r ? ru.report.period(formatDate(r.period), formatDate(r.period_end)) : undefined}
        actions={
          <Button
            variant="secondary"
            icon={<Download size={18} aria-hidden="true" />}
            loading={exp.isPending}
            onClick={() => exp.mutate(month, { onSuccess: () => toast({ message: ru.report.downloaded, tone: 'success' }) })}
          >
            {ru.report.download}
          </Button>
        }
      />
      <div className={s.month}>
        <MonthSwitcher month={month} max={now} onChange={(m) => setParams(m === now ? {} : { month: m }, { replace: true })} />
      </div>

      {report.isPending ? (
        <SkeletonList rows={3} height={120} />
      ) : report.isError ? (
        <ErrorState message={errorMessage(report.error)} onRetry={() => void report.refetch()} />
      ) : r ? (
        <div className={s.grid} aria-busy={report.isPlaceholderData}>
          <Card className={s.big}>
            <span className={layout.muted}>{ru.report.charged}</span>
            <MoneyText tyiyn={r.charged} className={s.value} />
          </Card>
          <Card className={s.big}>
            <span className={layout.muted}>{ru.report.collected}</span>
            <MoneyText tyiyn={r.collected.total} className={s.value} />
          </Card>
          <Card className={`${s.big} ${r.debt_end > 0 ? s.debt : ''}`}>
            <span className={layout.muted}>{ru.report.debtEnd}</span>
            <MoneyText tyiyn={r.debt_end} tone={r.debt_end > 0 ? 'debt' : undefined} className={s.value} />
            <span className={layout.muted}>{ru.report.debtors(r.debtors_end)}</span>
            {r.debt_end_note && <span className={s.note}>{r.debt_end_note}</span>}
          </Card>

          <Card className={s.wide}>
            <CardHeader title={ru.report.byMethod} />
            <dl className={s.list}>
              {(['cash', 'transfer', 'card', 'finik'] as const).map((m) => (
                <div key={m} className={s.line}>
                  <dt>{ru.report.methods[m]}</dt>
                  <dd>
                    <MoneyText tyiyn={r.collected[m]} />
                  </dd>
                </div>
              ))}
              <div className={s.line}>
                <dt>{ru.report.refunds}</dt>
                <dd>
                  <MoneyText tyiyn={-r.refunds} sign />
                </dd>
              </div>
              <div className={`${s.line} ${s.total}`}>
                <dt>{ru.report.collectedNet}</dt>
                <dd>
                  <MoneyText tyiyn={r.collected_net} />
                </dd>
              </div>
              <div className={s.line}>
                <dt>{ru.report.corrections}</dt>
                <dd>
                  <MoneyText tyiyn={r.corrections} sign />
                </dd>
              </div>
            </dl>
          </Card>

          <Card className={s.wide}>
            <CardHeader title={ru.report.students} />
            <dl className={s.list}>
              <div className={s.line}>
                <dt>{ru.report.active}</dt>
                <dd className="num">{r.students_active_end}</dd>
              </div>
              <div className={s.line}>
                <dt>{ru.report.joined}</dt>
                <dd className="num">{r.joined}</dd>
              </div>
              <div className={s.line}>
                <dt>{ru.report.left}</dt>
                <dd className="num">{r.left}</dd>
              </div>
            </dl>
          </Card>
        </div>
      ) : null}
    </>
  )
}
