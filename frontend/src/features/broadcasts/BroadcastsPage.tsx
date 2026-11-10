import { zodResolver } from '@hookform/resolvers/zod'
import { Megaphone } from 'lucide-react'
import { useState } from 'react'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { useSearchParams } from 'react-router'
import { z } from 'zod'
import { newIdempotencyKey } from '@/api/client'
import { useCenter } from '@/app/center'
import shared from '@/components/shared.module.css'
import { Button, Card, CardHeader, Checkbox, Dialog, EmptyState, ErrorState, FormError, PageHeader, RadioGroup, SkeletonList, Textarea, layout, useToast } from '@/components/ui'
import { useGroups } from '@/features/catalog/api'
import { WhatsAppLinkRow } from '@/features/whatsapp/WhatsAppLinkRow'
import waStyles from '@/features/whatsapp/WhatsApp.module.css'
import { ru } from '@/i18n/ru'
import { formatDateTime } from '@/utils/date'
import { applyFieldError, errorMessage } from '@/utils/errors'
import { useBroadcast, useBroadcasts, useCreateBroadcast, useOpenBroadcastLink, usePreviewBroadcast, type Broadcast, type BroadcastPreview, type BroadcastRequest } from './api'
import s from './Broadcasts.module.css'

const MAX_TEXT = 1000
const t = ru.broadcasts

const schema = z
  .object({
    audience: z.enum(['center', 'groups']),
    groups: z.array(z.string()),
    text: z.string().trim().min(1, ru.errors.required).max(MAX_TEXT, ru.errors.tooLong(MAX_TEXT)),
  })
  .superRefine((v, ctx) => {
    if (v.audience === 'groups' && v.groups.length === 0) ctx.addIssue({ code: 'custom', path: ['groups'], message: t.pickGroup })
  })
type Values = z.infer<typeof schema>

function toBody(v: Values): BroadcastRequest {
  return v.audience === 'groups' ? { audience: 'groups', group_ids: v.groups, text: v.text } : { audience: 'center', text: v.text }
}

function Stat({ label, value, note }: { label: string; value: string | number; note?: string }) {
  return (
    <div className={s.stat}>
      <span className={s.statLabel}>{label}</span>
      <span className={s.statValue}>{value}</span>
      {note && <span className={s.statNote}>{note}</span>}
    </div>
  )
}

/** Итоги и список ссылок. Хранятся на сервере: вкладка WhatsApp, перезагрузка и закрытие окна их не теряют. */
function ResultDialog({ id, onClose }: { id: string | null; onClose: () => void }) {
  const c = useCenter()
  const detail = useBroadcast(c.id, id)
  const open = useOpenBroadcastLink(c.id, id ?? '')
  const [opened, setOpened] = useState<Set<string>>(new Set())
  const d = detail.data
  const links = d?.links ?? []
  const withUrl = links.filter((l) => l.url)
  const noContact = links.filter((l) => !l.url)
  const isOpened = (l: (typeof links)[number]) => l.opened || opened.has(l.parent_id)
  const openedCount = withUrl.filter(isOpened).length

  return (
    <Dialog open={id !== null} onOpenChange={(o) => !o && onClose()} title={t.resultTitle} wide footer={<Button onClick={onClose}>{ru.common.close}</Button>}>
      {detail.isPending ? (
        <SkeletonList rows={3} />
      ) : detail.isError ? (
        <ErrorState message={errorMessage(detail.error)} onRetry={() => void detail.refetch()} />
      ) : (
        d && (
          <div className={layout.stack}>
            <div className={s.stats} data-testid="broadcast-stats">
              <Stat label={t.statTelegram} value={d.counts.telegram_sent} note={d.counts.telegram_queued > 0 ? t.statQueued(d.counts.telegram_queued) : undefined} />
              <Stat label={t.statWhatsApp} value={t.statOf(openedCount, d.counts.whatsapp_total)} />
              <Stat label={t.statNoContact} value={d.counts.no_contact} />
            </div>
            <p className={s.message}>{d.text}</p>
            {withUrl.length > 0 && (
              <section>
                <h3 className={s.subTitle}>{t.linksTitle}</h3>
                <ul className={waStyles.list} aria-label={t.linksTitle}>
                  {withUrl.map((l) => (
                    <WhatsAppLinkRow
                      key={l.parent_id}
                      title={l.parent_name}
                      meta={l.students.join(', ')}
                      url={l.url}
                      text={l.text}
                      opened={isOpened(l)}
                      onOpen={() => {
                        setOpened((prev) => new Set(prev).add(l.parent_id))
                        open.mutate(l.parent_id, { onSuccess: () => void detail.refetch() })
                      }}
                    />
                  ))}
                </ul>
                <p className={waStyles.note}>{ru.whatsapp.notGuaranteed}</p>
              </section>
            )}
            {noContact.length > 0 && (
              <section>
                <h3 className={s.subTitle}>{t.noContactTitle}</h3>
                <ul className={waStyles.list} aria-label={t.noContactTitle}>
                  {noContact.map((l) => (
                    <WhatsAppLinkRow key={l.parent_id} title={l.parent_name} meta={l.students.join(', ')} url={null} text={l.text} opened={false} onOpen={() => undefined} />
                  ))}
                </ul>
              </section>
            )}
          </div>
        )
      )}
    </Dialog>
  )
}

function HistoryRow({ b, onOpen }: { b: Broadcast; onOpen: () => void }) {
  return (
    <li className={shared.row}>
      <div className={shared.rowMain}>
        <span className={shared.rowTitle}>{b.audience === 'center' ? t.toCenter : b.group_names.join(', ')}</span>
        <span className={shared.rowMeta}>{t.by(formatDateTime(b.created_at), b.author)}</span>
        <span className={s.historyText}>{b.text}</span>
        <span className={shared.rowMeta}>{t.summary(b.counts.telegram_sent, b.counts.whatsapp_opened, b.counts.whatsapp_total, b.counts.no_contact)}</span>
      </div>
      <div className={shared.rowSide}>
        <Button variant="secondary" onClick={onOpen} aria-label={`${t.open}: ${formatDateTime(b.created_at)}`}>
          {t.open}
        </Button>
      </div>
    </li>
  )
}

export function BroadcastsPage() {
  const c = useCenter()
  const groups = useGroups(c.id)
  const history = useBroadcasts(c.id)
  const preview = usePreviewBroadcast(c.id)
  const create = useCreateBroadcast(c.id)
  const toast = useToast()
  const [params, setParams] = useSearchParams()
  const resultId = params.get('open')
  const showResult = (id: string | null) => setParams(id ? { open: id } : {}, { replace: true })

  const {
    control,
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors },
  } = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { audience: 'center', groups: [], text: '' } })
  const audience = useWatch({ control, name: 'audience' })
  const text = useWatch({ control, name: 'text' })
  const [draft, setDraft] = useState<{ body: BroadcastRequest; preview: BroadcastPreview; key: string } | null>(null)
  const [formError, setFormError] = useState<string>()
  const activeGroups = (groups.data ?? []).filter((g) => !g.archived)

  const openPreview = handleSubmit((v) => {
    setFormError(undefined)
    const body = toBody(v)
    preview.mutate(body, {
      onSuccess: (p) => setDraft({ body, preview: p, key: newIdempotencyKey() }),
      onError: (e) => {
        if (!applyFieldError(e, setError, ['text', 'audience'])) setFormError(errorMessage(e))
      },
    })
  })

  const send = () => {
    if (!draft) return
    create.mutate(
      { body: draft.body, key: draft.key },
      {
        onSuccess: (b) => {
          setDraft(null)
          reset()
          showResult(b.id)
        },
        onError: (e) => {
          setDraft(null)
          toast({ message: errorMessage(e), tone: 'error' })
        },
      },
    )
  }

  return (
    <>
      <PageHeader title={t.title} subtitle={t.subtitle} />
      {c.isDemo && <p className={s.demo}>{t.demo}</p>}

      <Card>
        <form className={layout.stack} onSubmit={(e) => void openPreview(e)} noValidate>
          <Controller
            control={control}
            name="audience"
            render={({ field }) => (
              <RadioGroup
                legend={t.audience}
                name="audience"
                value={field.value}
                onChange={field.onChange}
                options={[
                  { value: 'center', label: t.audienceCenter, hint: t.audienceCenterHint },
                  { value: 'groups', label: t.audienceGroups },
                ]}
              />
            )}
          />
          {audience === 'groups' && (
            <fieldset className={s.groups}>
              <legend className={s.subTitle}>{t.groups}</legend>
              {activeGroups.length === 0 && <p className={layout.muted}>{t.noGroups}</p>}
              <Controller
                control={control}
                name="groups"
                render={({ field }) => (
                  <>
                    {activeGroups.map((g) => (
                      <Checkbox
                        key={g.id}
                        label={g.name}
                        checked={field.value.includes(g.id)}
                        onChange={(e) => field.onChange(e.target.checked ? [...field.value, g.id] : field.value.filter((x) => x !== g.id))}
                      />
                    ))}
                  </>
                )}
              />
              {errors.groups?.message && <FormError>{errors.groups.message}</FormError>}
            </fieldset>
          )}
          <Textarea label={t.text} rows={5} maxLength={MAX_TEXT} hint={t.counter(text.length, MAX_TEXT)} error={errors.text?.message} {...register('text')} />
          <FormError>{formError}</FormError>
          <div className={shared.actions}>
            <Button type="submit" loading={preview.isPending}>
              {t.preview}
            </Button>
          </div>
        </form>
      </Card>

      <Card flush className={s.history}>
        <div className={s.historyHead}>
          <CardHeader title={t.historyTitle} />
        </div>
        {history.isPending ? (
          <SkeletonList rows={2} />
        ) : history.isError ? (
          <ErrorState message={errorMessage(history.error)} onRetry={() => void history.refetch()} />
        ) : history.data.items.length === 0 ? (
          <EmptyState icon={<Megaphone aria-hidden="true" />} title={t.historyEmpty} text={t.historyEmptyText} />
        ) : (
          <ul className={shared.list} aria-label={t.historyTitle}>
            {history.data.items.map((b) => (
              <HistoryRow key={b.id} b={b} onOpen={() => showResult(b.id)} />
            ))}
          </ul>
        )}
      </Card>

      <Dialog
        open={draft !== null}
        onOpenChange={(o) => !o && setDraft(null)}
        title={t.previewTitle}
        description={c.isDemo ? t.demo : undefined}
        footer={
          <>
            <Button variant="secondary" onClick={() => setDraft(null)}>
              {t.back}
            </Button>
            <Button loading={create.isPending} disabled={c.isDemo} onClick={send}>
              {t.send}
            </Button>
          </>
        }
      >
        {draft && (
          <div className={layout.stack}>
            <h3 className={s.subTitle}>{t.recipients}</h3>
            <div className={s.stats} data-testid="broadcast-preview">
              <Stat label={t.previewTelegram} value={draft.preview.telegram} />
              <Stat label={t.previewWhatsApp} value={draft.preview.whatsapp} />
              <Stat label={t.previewNoContact} value={draft.preview.no_contact} />
            </div>
            <h3 className={s.subTitle}>{t.message}</h3>
            <p className={s.message}>{draft.preview.telegram_text}</p>
          </div>
        )}
      </Dialog>

      <ResultDialog id={resultId} onClose={() => showResult(null)} />
    </>
  )
}
