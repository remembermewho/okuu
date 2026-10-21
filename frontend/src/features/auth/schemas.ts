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

export const registerSchema = z.object({
  name: nameField(),
  center_name: nameField(),
  phone: phoneField,
  password: passwordField,
})
export type RegisterForm = z.infer<typeof registerSchema>
