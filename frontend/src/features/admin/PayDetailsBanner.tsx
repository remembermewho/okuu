import { AlertTriangle } from 'lucide-react'
import { Link } from 'react-router'
import { centerPath, useCenter } from '@/app/center'
import { can } from '@/app/permissions'
import { ru } from '@/i18n/ru'
import { useSettings } from './api'
import s from './PayDetailsBanner.module.css'

/**
 * «Заполните реквизиты для оплаты»: без них бот не скажет родителям, куда платить. Показывается, пока
 * pay_details пусты — в «Настройках» и на «Должниках». Владельцу — со ссылкой на настройки
 * (withLink=false — уже на них), администратору — без ссылки: реквизиты меняет только владелец.
 */
export function PayDetailsBanner({ withLink = true }: { withLink?: boolean }) {
  const c = useCenter()
  const settings = useSettings(c.id)
  if (!settings.data || (settings.data.pay_details ?? '').trim() !== '') return null
  const owner = can.manageStaff(c.role)
  return (
    <div className={s.banner} role="status">
      <AlertTriangle size={20} aria-hidden="true" />
      <p>
        {ru.payDetails.missing}
        {owner && withLink && (
          <>
            {' '}
            <Link to={centerPath(c.id, 'settings')}>{ru.payDetails.fill}</Link>
          </>
        )}
        {!owner && ` ${ru.payDetails.askOwner}`}
      </p>
    </div>
  )
}
