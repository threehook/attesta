// The look and feel shared with the other Attesta apps: header with logo and breadcrumb, menu on the left, one-line footer.
export interface Crumb {
  label: string;
  href?: string;
}

export interface MenuItem {
  id: string;
  label: string;
  href: string;
  description: string;
}

/** The first menu item, without a label above it. */
export const HOME: MenuItem = { id: "home", label: "Home", href: "#/", description: "" };

export const MENU: { label: string; items: MenuItem[] }[] = [
  {
    label: "Mijn wallet",
    items: [
      {
        id: "credentials",
        label: "Credentials",
        href: "#/credentials",
        description: "Bekijk de credentials in uw wallet en wat ze over u zeggen.",
      },
      {
        id: "link",
        label: "Link openen",
        href: "#/link",
        description: "Open een aanbod van een uitgever of een verzoek van een applicatie, als de link niet vanzelf in de wallet opent.",
      },
      {
        id: "delen",
        label: "Delen",
        href: "#/delen",
        description: "Bekijk en beheer de applicaties waarbij u automatisch aanmeldt of waarmee u altijd deelt.",
      },
    ],
  },
];
