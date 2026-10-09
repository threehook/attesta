// The look and feel shared with the other Attesta example apps: header with logo and breadcrumb, menu on the left, one-line footer.
export interface Crumb {
  label: string
  href?: string
}

export interface MenuItem {
  id: string
  label: string
  href: string
  description: string
}

/** The first menu item, without a label above it. */
export const HOME: MenuItem = { id: 'home', label: 'Home', href: '#/', description: '' }

export const MENU: { label: string; items: MenuItem[] }[] = [
  {
    label: 'Aanvragen',
    items: [
      {
        id: 'laadpaal',
        label: 'Laadpaal aanvragen',
        href: '#/laadpaal',
        description: 'Dien een aanvraag in voor een laadpaal bij het adres van een burger. U meldt u aan met het medewerker-ID uit uw wallet.',
      },
    ],
  },
]
