/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Link } from '@tanstack/react-router'
import {
  ArrowUpRight,
  BookOpen,
  Image,
  KeyRound,
  Video,
  Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Footer } from '@/components/layout/components/footer'
import { Button } from '@/components/ui/button'
import { appPath } from '@/lib/app-path'

export function QisiHome() {
  const { t } = useTranslation()
  const endpoint = `${window.location.origin}${appPath('/likeai/task/create_task')}`
  const sample = `curl ${endpoint} \\\n  -H "Authorization: Bearer $QISI_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{\n    "model": "doubao_seedance_2_5",\n    "prompt": "A quiet seaside morning",\n    "duration": 5,\n    "resolution": "720p"\n  }'`
  return (
    <>
      <main className='mx-auto max-w-6xl px-6 py-16 md:py-24'>
        <section className='grid gap-12 border-b pb-16 lg:grid-cols-2 lg:items-center'>
          <div>
            <p className='text-muted-foreground mb-5 text-xs font-medium tracking-[0.2em]'>
              QISI API · {t('Creative infrastructure')}
            </p>
            <h1 className='text-5xl leading-tight font-semibold tracking-tight md:text-6xl'>
              {t('Your next creation,')}
              <br />
              <span className='text-amber-600 dark:text-amber-400'>
                {t('one API away.')}
              </span>
            </h1>
            <p className='text-muted-foreground mt-6 max-w-lg text-base leading-8'>
              {t(
                'Generate images and videos with one key. Keep your projects local, and manage your usage in one place.'
              )}
            </p>
            <div className='mt-8 flex flex-wrap gap-3'>
              <Button
                size='lg'
                className='h-11 px-5'
                render={<Link to='/wallet' />}
              >
                {t('Open workspace')}
                <ArrowUpRight />
              </Button>
              <Button
                variant='outline'
                size='lg'
                className='h-11 px-5'
                render={<a href={appPath('/qisi-guide.html')} />}
              >
                {t('Integration guide')}
                <BookOpen />
              </Button>
            </div>
          </div>
          <div className='overflow-hidden rounded-2xl border bg-slate-950 text-slate-200 shadow-xl'>
            <div className='flex items-center justify-between border-b border-white/10 px-5 py-3 text-xs'>
              <span>POST /likeai/task/create_task</span>
              <CopyButton
                value={sample}
                className='text-slate-300 hover:text-white'
              />
            </div>
            <pre className='overflow-x-auto p-6 text-xs leading-7'>
              <code>{sample}</code>
            </pre>
            <p className='border-t border-white/10 px-5 py-3 text-xs text-slate-400'>
              {t('Create a task, then retrieve its result with your task ID.')}
            </p>
          </div>
        </section>
        <section className='py-12'>
          <div className='mb-7 flex items-end justify-between gap-5'>
            <div>
              <p className='text-muted-foreground mb-2 text-xs tracking-widest'>
                FOR CREATORS
              </p>
              <h2 className='text-2xl font-semibold'>
                {t('From a still image to a story.')}
              </h2>
            </div>
            <Button variant='ghost' render={<Link to='/pricing' />}>
              {t('Models and pricing')}
              <ArrowUpRight />
            </Button>
          </div>
          <div className='grid gap-5 md:grid-cols-2'>
            <article className='rounded-xl border p-7'>
              <Image className='mb-6 size-7 text-amber-600' />
              <p className='text-muted-foreground text-xs'>
                {t('Image generation')}
              </p>
              <h3 className='mt-2 text-xl font-medium'>Seedream 4.5</h3>
              <p className='text-muted-foreground mt-3 text-sm leading-7'>
                {t(
                  'Turn prompts and reference images into visual assets for your next project.'
                )}
              </p>
            </article>
            <article className='rounded-xl border p-7'>
              <Video className='mb-6 size-7 text-amber-600' />
              <p className='text-muted-foreground text-xs'>
                {t('Video generation')}
              </p>
              <h3 className='mt-2 text-xl font-medium'>Seedance 2.5</h3>
              <p className='text-muted-foreground mt-3 text-sm leading-7'>
                {t(
                  'Create videos from text or images, with a specified duration and resolution.'
                )}
              </p>
            </article>
          </div>
          <p className='text-muted-foreground mt-4 text-xs'>
            {t(
              'Available models and current prices are shown on the pricing page. Unconfigured models cannot be submitted.'
            )}
          </p>
        </section>
        <section className='grid gap-8 border-t py-12 md:grid-cols-3'>
          {[
            {
              icon: KeyRound,
              title: 'Your keys, your limits',
              text: 'Create separate API keys for applications and keep access under control.',
            },
            {
              icon: Wallet,
              title: 'A clear account of every call',
              text: 'View your balance, top-ups and usage history in the workspace.',
            },
            {
              icon: BookOpen,
              title: 'Works with your workflow',
              text: 'Connect your applications and local Agents through the documented task API.',
            },
          ].map((item) => (
            <article key={item.title}>
              <item.icon className='mb-4 size-5' />
              <h3 className='font-medium'>{t(item.title)}</h3>
              <p className='text-muted-foreground mt-3 text-sm leading-7'>
                {t(item.text)}
              </p>
            </article>
          ))}
        </section>
        <div className='bg-muted flex flex-wrap items-center justify-between gap-5 rounded-xl p-7'>
          <div>
            <h2 className='font-medium'>{t('Your canvas stays yours.')}</h2>
            <p className='text-muted-foreground mt-2 text-sm'>
              {t(
                'Use qisiTV for local projects. This workspace manages API access and billing.'
              )}
            </p>
          </div>
          <Button
            variant='outline'
            render={<a href='https://cheeser.link/qisitv/' />}
          >
            qisiTV
            <ArrowUpRight />
          </Button>
        </div>
      </main>
      <Footer columns={[]} />
    </>
  )
}
