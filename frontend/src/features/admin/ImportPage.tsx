import { CheckCircle2, Download, FileSpreadsheet, Upload } from 'lucide-react'
import { useId, useRef, useState } from 'react'
import type { Schemas } from '@/api/client'
import { centerPath, useCenter } from '@/app/center'
import { Badge, Button, ButtonLink, Card, CardHeader, FormError, PageHeader, RadioGroup, Select, layout, useToast } from '@/components/ui'
import { ru } from '@/i18n/ru'
import { cx } from '@/utils/cx'
import { formatDate, formatDateTime } from '@/utils/date'
import { errorMessage, isApiError } from '@/utils/errors'
import { formatSom } from '@/utils/money'
import { formatPhone } from '@/utils/phone'
import { useImportCommit, useImportPreview, useImportTemplate, type ImportPreview, type ImportRow } from './api'
import s from './Import.module.css'

const MAX_BYTES = 5 * 1024 * 1024
type RowFilter = 'all' | 'problems' | 'error'
type FirstMonth = 'full' | 'none'

export function ImportPage() {
  const c = useCenter()
  const template = useImportTemplate(c.id)
  const preview = useImportPreview(c.id)
  const commit = useImportCommit(c.id)
  const toast = useToast()
  const fileId = useId()
  const input = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [fileError, setFileError] = useState<string>()
  const [data, setData] = useState<ImportPreview | null>(null)
  const [result, setResult] = useState<Schemas['ImportResult'] | null>(null)
  const [firstMonth, setFirstMonth] = useState<FirstMonth>('full')
  const [commitError, setCommitError] = useState<string>()

  const reset = () => {
    setFile(null)
    setData(null)
    setResult(null)
    setFileError(undefined)
    setCommitError(undefined)
    setFirstMonth('full')
    if (input.current) input.current.value = ''
  }

  const check = () => {
    if (!file) return setFileError(ru.import.noFile)
    if (file.size > MAX_BYTES) return setFileError(ru.import.tooLarge)
    setFileError(undefined)
    preview.mutate(file, {
      onSuccess: (p) => setData(p),
      onError: (err) => setFileError(errorMessage(err)),
    })
  }

  const confirm = () => {
    if (!data) return
    setCommitError(undefined)
    commit.mutate(
      { preview_id: data.preview_id, first_month: firstMonth },
      {
        onSuccess: (r) => {
          setResult(r)
          setData(null)
          toast({ message: ru.import.resultTitle, tone: 'success' })
        },
        onError: (err) => setCommitError(isApiError(err, 'not_found') ? ru.import.expired : errorMessage(err)),
      },
    )
  }

  return (
    <>
      <PageHeader title={ru.import.title} subtitle={ru.import.lead} />
      {result ? (
        <ResultCard result={result} onMore={reset} />
      ) : data ? (
        <PreviewCard
          data={data}
          firstMonth={firstMonth}
          onFirstMonth={setFirstMonth}
          onCommit={confirm}
          onAgain={reset}
          committing={commit.isPending}
          error={commitError}
        />
      ) : (
        <div className={s.steps}>
          <Card>
            <CardHeader title={`1. ${ru.import.step1}`} />
            <p className={layout.muted}>{ru.import.step1Text}</p>
            <Button variant="secondary" icon={<Download size={18} aria-hidden="true" />} loading={template.isPending} onClick={() => template.mutate()}>
              {ru.import.template}
            </Button>
          </Card>
          <Card>
            <CardHeader title={`2. ${ru.import.step2}`} />
            <p className={layout.muted}>{ru.import.step2Text}</p>
            <div className={s.upload}>
              <label htmlFor={fileId} className={s.drop}>
                <FileSpreadsheet size={28} aria-hidden="true" />
                <span className={s.dropText}>{file ? file.name : ru.import.choose}</span>
                <input
                  ref={input}
                  id={fileId}
                  className={s.fileInput}
                  type="file"
                  accept=".xlsx,.csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv"
                  aria-label={ru.import.file}
                  aria-invalid={fileError ? true : undefined}
                  aria-describedby={fileError ? `${fileId}-error` : undefined}
                  onChange={(e) => {
                    setFile(e.target.files?.[0] ?? null)
                    setFileError(undefined)
                  }}
                />
              </label>
              {fileError && (
                <span id={`${fileId}-error`} className={s.fileError} role="alert">
                  {fileError}
                </span>
              )}
              <Button icon={<Upload size={18} aria-hidden="true" />} loading={preview.isPending} onClick={check}>
                {preview.isPending ? ru.import.uploading : ru.import.upload}
              </Button>
            </div>
          </Card>
        </div>
      )}
    </>
  )
}

function rowState(r: ImportRow): 'ok' | 'warning' | 'error' | 'skip' {
  return r.skip ? 'skip' : r.status
}

function PreviewCard({
  data,
  firstMonth,
  onFirstMonth,
  onCommit,
  onAgain,
  committing,
  error,
}: {
  data: ImportPreview
  firstMonth: FirstMonth
  onFirstMonth: (v: FirstMonth) => void
  onCommit: () => void
  onAgain: () => void
  committing: boolean
  error?: string
}) {
  const [filter, setFilter] = useState<RowFilter>(data.summary.error + data.summary.warning > 0 ? 'problems' : 'all')
  const sum = data.summary
  const rows = data.rows.filter((r) => (filter === 'all' ? true : filter === 'error' ? r.status === 'error' : r.status !== 'ok' || r.skip))
  const hasMonthly = data.rows.some((r) => !r.skip && r.status !== 'error' && r.tariff_kind === 'monthly')

  return (
    <div className={layout.stack}>
      <Card>
        <CardHeader title={ru.import.previewTitle} />
        <p>{ru.import.summary(sum.total)}</p>
        <ul className={s.chips}>
          <li className={cx(s.chip, s.chip_ok)}>
            {sum.ok} {ru.import.ok}
          </li>
          <li className={cx(s.chip, s.chip_warning)}>
            {sum.warning} {ru.import.warning}
          </li>
          <li className={cx(s.chip, s.chip_error)}>
            {sum.error} {ru.import.error}
          </li>
          <li className={cx(s.chip, s.chip_skip)}>
            {sum.skipped} {ru.import.skipped}
          </li>
        </ul>
        <p className={s.toImport}>{ru.import.toImport(sum.to_import)}</p>
        {sum.error > 0 && <p className={layout.muted}>{ru.import.errorsNote}</p>}
        <p className={layout.muted}>{ru.import.expires(formatDateTime(data.expires_at))}</p>
      </Card>

      <Card flush>
        <div className={s.tableHead}>
          <Select
            label={ru.import.filter}
            value={filter}
            onChange={(e) => setFilter(e.target.value as RowFilter)}
            options={(['all', 'problems', 'error'] as const).map((f) => ({ value: f, label: ru.import.filters[f] ?? f }))}
          />
        </div>
        <div className={s.tableWrap}>
          <table className={s.table}>
            <thead>
              <tr>
                <th scope="col">{ru.import.cols.row}</th>
                <th scope="col">{ru.import.cols.student}</th>
                <th scope="col">{ru.import.cols.parent}</th>
                <th scope="col">{ru.import.cols.group}</th>
                <th scope="col" className={s.num}>
                  {ru.import.cols.debt}
                </th>
                <th scope="col">{ru.import.cols.status}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => {
                const st = rowState(r)
                return (
                  <tr key={r.row} className={s[`row_${st}`]}>
                    <td className={s.num} data-label={ru.import.cols.row}>
                      {r.row}
                    </td>
                    <td data-label={ru.import.cols.student}>
                      <strong>{r.full_name}</strong>
                      {r.messages && r.messages.length > 0 && (
                        <ul className={s.messages}>
                          {r.messages.map((m, i) => (
                            <li key={i}>{m}</li>
                          ))}
                        </ul>
                      )}
                    </td>
                    <td data-label={ru.import.cols.parent}>
                      {r.parent_name}
                      {r.parent_phone && <div className={s.sub}>{formatPhone(r.parent_phone)}</div>}
                    </td>
                    <td data-label={ru.import.cols.group}>
                      {r.group}
                      {r.group && !r.group_id && <span className={s.sub}>{` (${ru.import.newGroup})`}</span>}
                      <div className={s.sub}>
                        {r.tariff_id ? r.tariff : r.tariff && `${r.tariff} — ${ru.import.noTariff}`}
                        {r.lessons_left !== null && ` · ${ru.import.lessonsLeft(r.lessons_left)}`}
                        {r.start_date && ` · ${ru.import.since(formatDate(r.start_date))}`}
                      </div>
                    </td>
                    <td className={s.num} data-label={ru.import.cols.debt}>
                      {r.debt > 0 ? formatSom(r.debt) : ru.common.none}
                    </td>
                    <td data-label={ru.import.cols.status}>
                      <Badge tone={st === 'ok' ? 'success' : st === 'error' || st === 'warning' ? 'debt' : 'neutral'} className={s[`badge_${st}`]}>
                        {ru.import.rowStatus[st]}
                      </Badge>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </Card>

      <Card>
        {hasMonthly && (
          <RadioGroup
            legend={ru.import.firstMonth}
            name="first_month"
            value={firstMonth}
            onChange={(v) => onFirstMonth(v as FirstMonth)}
            options={(['full', 'none'] as const).map((v) => ({ value: v, label: ru.import.firstMonthOptions[v] }))}
          />
        )}
        {hasMonthly && <p className={layout.muted}>{ru.import.firstMonthHint}</p>}
        <FormError>{sum.to_import === 0 ? ru.import.nothing : error}</FormError>
        <div className={s.actions}>
          <Button size="lg" loading={committing} disabled={sum.to_import === 0} onClick={onCommit}>
            {ru.import.commit(sum.to_import)}
          </Button>
          <Button variant="secondary" size="lg" onClick={onAgain}>
            {ru.import.again}
          </Button>
        </div>
      </Card>
    </div>
  )
}

function ResultCard({ result, onMore }: { result: Schemas['ImportResult']; onMore: () => void }) {
  const c = useCenter()
  const r = ru.import.result
  return (
    <Card>
      <div className={s.resultHead}>
        <CheckCircle2 size={28} aria-hidden="true" />
        <h2>{ru.import.resultTitle}</h2>
      </div>
      <ul className={s.resultList}>
        <li>{r.students(result.students_created)}</li>
        <li>{r.parents(result.parents_created)}</li>
        <li>{r.enrollments(result.enrollments_created)}</li>
        {result.groups_created > 0 && <li>{r.groups(result.groups_created)}</li>}
        {result.skipped > 0 && <li>{r.skipped(result.skipped)}</li>}
        {result.debt_total > 0 && <li>{r.debt(formatSom(result.debt_total))}</li>}
      </ul>
      <div className={s.actions}>
        <ButtonLink to={centerPath(c.id, 'students')}>{ru.import.toStudents}</ButtonLink>
        {result.debt_total > 0 && (
          <ButtonLink variant="secondary" to={centerPath(c.id, 'debtors')}>
            {ru.import.toDebtors}
          </ButtonLink>
        )}
        <Button variant="ghost" onClick={onMore}>
          {ru.import.more}
        </Button>
      </div>
    </Card>
  )
}
