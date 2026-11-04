import {clsx, type ClassValue} from 'clsx'
import {twMerge} from 'tailwind-merge'

/** Merge conditional classes while retaining Tailwind's last-class-wins behavior. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
