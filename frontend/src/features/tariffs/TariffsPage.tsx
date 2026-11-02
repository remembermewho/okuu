import { zodResolver } from '@hookform/resolvers/zod'
import { Archive, Pencil, Plus, Tag } from 'lucide-react'
import { useState } from 'react'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { useSearchParams } from 'react-router'
import { z } from 'zod'
import { useCenter } from '@/app/center'
import shared from '@/components/shared.module.css'
import { Badge, Button, Card, Checkbox, ConfirmDialog, Dialog, EmptyState, ErrorState, FormError, Input, MoneyText, PageHeader, RadioGroup, SkeletonList, layout, useToast } from '@/components/ui'
import { useArchiveTariff, useCreateTariff, usePatchTariff, useTariffs, type Tariff } from '@/features/catalog/api'
import { ru } from '@/i18n/ru'
import { applyFieldError, errorMessage } from '@/utils/errors'
import { parseSom, tyiynToInput } from '@/utils/money'
import s from './Tariffs.module.css'

const schema = z
  .object({
    name: z.string().trim().min(1, ru.errors.required).max(100, ru.errors.tooLong(100)),
    kind: z.enum(['monthly', 'package']),
    price: z.string(),
    lessons: z.string(),
  })
  .superRefine((v, ctx) => {
    const p = parseSom(v.price)
    if (p === null) ctx.addIssue({ code: 'custom', path: ['price'], message: ru.errors.invalidAmount })
    if (v.kind === 'package') {
      const n = Number(v.lessons)
      if (!/^\d+$/.test(v.lessons.trim()) || n < 1 || n > 1000) ctx.addIssue({ code: 'custom', path: ['lessons'], message: ru.errors.range(1, 1000) })
    }
  })
type Values = z.infer<typeof schema>

export function TariffsPage() {
  const c = useCenter()
  const [params, setParams] = useSearchParams()
  const archived = params.get('archived') === '1'
  const tariffs = useTariffs(c.id, archived)
  const archive = useArchiveTariff(c.id)
  const toast = useToast()
  const [editing, setEditing] = useState<Tariff | null | undefined>(undefined)
  const [archiving, setArchiving] = useState<Tariff | null>(null)

  return (
    <>
      <PageHeader
        title={ru.tariffs.title}
        actions={
          <Button icon={<Plus size={18} aria-hidden="true" />} onClick={() => setEditing(null)}>
            {ru.tariffs.add}
          </Button>
        }
      />
      <div className={s.toolbar}>
        <Checkbox label={ru.common.showArchived} checked={archived} onChange={(e) => setParams(e.target.checked ? { archived: '1' } : {}, { replace: true })} />
      </div>
      {tariffs.isPending ? (
        <SkeletonList rows={3} />
      ) : tariffs.isError ? (
        <ErrorState message={errorMessage(tariffs.error)} onRetry={() => void tariffs.refetch()} />
      ) : tariffs.data.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Tag aria-hidden="true" />}
            title={archived ? ru.tariffs.emptyArchive : ru.tariffs.emptyTitle}
            text={archived ? undefined : ru.tariffs.emptyText}
            action={
              !archived && (
                <Button icon={<Plus size={18} aria-hidden="true" />} onClick={() => setEditing(null)}>
                  {ru.tariffs.add}
                </Button>
              )
            }
          />
        </Card>
      ) : (
        <Card flush>
          <ul className={shared.list} aria-label={ru.tariffs.title}>
            {tariffs.data.map((t) => (
              <li key={t.id} className={s.row}>
                <div className={shared.rowMain}>
                  <span className={shared.rowTitle}>
                    {t.name}
                    {t.archived && <Badge>{ru.common.archived}</Badge>}
                  </span>
                  {/* «В месяц» или «Абонемент · за 8 занятий» — без повтора «в месяц · в месяц». */}
                  <span className={shared.rowMeta}>
                    {ru.tariffs.kinds[t.kind]}
                    {t.kind === 'package' && t.lessons_count ? ` · ${ru.tariffs.perPackage(t.lessons_count)}` : ''}
                  </span>
                </div>
                <MoneyText tyiyn={t.price} className={shared.amount} />
                {!t.archived && (
                  <div className={s.rowActions}>
                    <Button size="sm" variant="ghost" iconOnly aria-label={`${ru.common.edit}: ${t.name}`} onClick={() => setEditing(t)}>
                      <Pencil size={18} aria-hidden="true" />
                    </Button>
                    <Button size="sm" variant="ghost" iconOnly aria-label={`${ru.common.archive}: ${t.name}`} onClick={() => setArchiving(t)}>
                      <Archive size={18} aria-hidden="true" />
                    </Button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        </Card>
      )}

      {editing !== undefined && <TariffDialog tariff={editing} onClose={() => setEditing(undefined)} />}
      <ConfirmDialog
        open={archiving !== null}
        onOpenChange={(o) => !o && setArchiving(null)}
        title={ru.tariffs.archiveTitle}
        text={ru.tariffs.archiveText}
        confirmLabel={ru.common.archive}
        danger
        loading={archive.isPending}
        onConfirm={() => archiving && archive.mutate(archiving.id, { onSuccess: () => (setArchiving(null), toast({ message: ru.tariffs.archived, tone: 'success' })) })}
      />
    </>
  )
}

function TariffDialog({ tariff, onClose }: { tariff: Tariff | null; onClose: () => void }) {
  const c = useCenter()
  const create = useCreateTariff(c.id)
  const patch = usePatchTariff(c.id)
  const toast = useToast()
  const [formError, setFormError] = useState<string>()
  const {
    register,
    control,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: tariff?.name ?? '',
      kind: tariff?.kind ?? 'monthly',
      price: tariff ? tyiynToInput(tariff.price) : '',
      lessons: tariff?.lessons_count ? String(tariff.lessons_count) : '',
    },
  })
  const kind = useWatch({ control, name: 'kind' })

  const onSubmit = handleSubmit((v) => {
    setFormError(undefined)
    const body = { name: v.name, kind: v.kind, price: parseSom(v.price) ?? 0, lessons_count: v.kind === 'package' ? Number(v.lessons) : null }
    const opts = {
      onSuccess: () => {
        toast({ message: tariff ? ru.tariffs.saved : ru.tariffs.created, tone: 'success' as const })
        onClose()
      },
      onError: (err: unknown) => {
        if (!applyFieldError(err, setError, { name: 'name', kind: 'kind', price: 'price', lessons_count: 'lessons' })) setFormError(errorMessage(err))
      },
    }
    if (tariff) patch.mutate({ id: tariff.id, body }, opts)
    else create.mutate(body, opts)
  })

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={tariff ? ru.tariffs.editTitle : ru.tariffs.createTitle}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {ru.common.cancel}
          </Button>
          <Button type="submit" form="tariff-form" loading={create.isPending || patch.isPending}>
            {tariff ? ru.common.save : ru.common.create}
          </Button>
        </>
      }
    >
      <form id="tariff-form" className={layout.stack} onSubmit={onSubmit} noValidate>
        <Input label={ru.tariffs.name} placeholder={ru.tariffs.namePlaceholder} error={errors.name?.message} autoFocus {...register('name')} />
        <Controller
          control={control}
          name="kind"
          render={({ field }) => (
            <RadioGroup
              legend={ru.tariffs.kind}
              name="kind"
              value={field.value}
              onChange={field.onChange}
              error={errors.kind?.message}
              options={(['monthly', 'package'] as const).map((k) => ({ value: k, label: ru.tariffs.kinds[k], hint: ru.tariffs.kindHints[k] }))}
            />
          )}
        />
        <div className={`${layout.formGrid} ${layout.formGrid2}`}>
          <Input label={ru.tariffs.price} inputMode="decimal" suffix={ru.common.som} hint={tariff ? ru.tariffs.priceHint : undefined} error={errors.price?.message} {...register('price')} />
          {kind === 'package' && <Input label={ru.tariffs.lessons} inputMode="numeric" error={errors.lessons?.message} {...register('lessons')} />}
        </div>
        <FormError>{formError}</FormError>
      </form>
    </Dialog>
  )
}
