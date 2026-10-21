import { z } from 'zod'
import { ru } from '@/i18n/ru'
import { isValidPhone } from '@/utils/phone'

export const phoneField = z.string().trim().min(1, ru.errors.required).refine(isValidPhone, ru.errors.invalidPhone)

export const passwordField = z.string().min(8, ru.errors.passwordMin).max(128, ru.errors.passwordMax)

export const nameField = (max = 100) => z.string().trim().min(1, ru.errors.required).max(max, ru.errors.tooLong(max))

export const loginSchema = z.object({
  phone: phoneField,
  password: z.string().min(1, ru.errors.required),
})
export type LoginForm = z.infer<typeof loginSchema>

/** Новый пароль с повтором (смена пароля, сброс по ссылке). */
export const newPasswordFields = {
  new_password: passwordField,
  repeat_password: z.string(),
}
export const checkRepeat = (v: { new_password: string; repeat_password: string }, ctx: z.RefinementCtx) => {
  if (v.repeat_password !== v.new_password) ctx.addIssue({ code: 'custom', path: ['repeat_password'], message: ru.account.passwordsDiffer })
}

export const changePasswordSchema = z
  .object({ current_password: z.string().min(1, ru.errors.required), ...newPasswordFields })
  .superRefine((v, ctx) => {
    checkRepeat(v, ctx)
    if (v.current_password && v.new_password === v.current_password) ctx.addIssue({ code: 'custom', path: ['new_password'], message: ru.account.samePassword })
  })
export type ChangePasswordForm = z.infer<typeof changePasswordSchema>

export const resetPasswordSchema = z.object(newPasswordFields).superRefine(checkRepeat)
export type ResetPasswordForm = z.infer<typeof resetPasswordSchema>

export const registerSchema = z.object({
  name: nameField(),
  center_name: nameField(),
  phone: phoneField,
  password: passwordField,
})
export type RegisterForm = z.infer<typeof registerSchema>
