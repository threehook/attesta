import type { ReactNode } from "react";
import { HOME, MENU, type Crumb, type MenuItem } from "./menu.js";

export function Shell(props: { title: string; crumbs: Crumb[]; active: string; footer?: ReactNode; children: ReactNode }) {
  const crumbs = [...props.crumbs, { label: props.title }];
  const list = (items: MenuItem[]) => (
    <ul>
      {items.map((item) => (
        <li key={item.id}>
          <a href={item.href} aria-current={item.id === props.active ? "page" : undefined}>
            {item.label}
          </a>
        </li>
      ))}
    </ul>
  );
  return (
    <div className="app">
      <header>
        <div className="logo">
          <img src="./logo.svg" alt="Gemeente Vlierdam" />
        </div>
        <div className="title">
          <nav className="crumbs" aria-label="Kruimelpad">
            <ol>
              {crumbs.map((crumb, i) =>
                i === crumbs.length - 1 ? (
                  <li key={i} aria-current="page">
                    {crumb.label}
                  </li>
                ) : (
                  <li key={i}>{crumb.href ? <a href={crumb.href}>{crumb.label}</a> : crumb.label}</li>
                ),
              )}
            </ol>
          </nav>
          <h1>{props.title}</h1>
        </div>
      </header>
      <nav className="menu" aria-label="Menu">
        {list([HOME])}
        {MENU.map((group) => (
          <div key={group.label}>
            <h2>{group.label}</h2>
            {list(group.items)}
          </div>
        ))}
      </nav>
      <main>{props.children}</main>
      <footer>{props.footer ?? " "}</footer>
    </div>
  );
}

export function Blocks() {
  return (
    <>
      <p className="intro">Maak een keuze in het menu om verder te gaan.</p>
      <div className="blocks">
        {MENU.flatMap((group) =>
          group.items.map((item) => (
            <a key={item.id} className="block" href={item.href}>
              <p className="group">{group.label}</p>
              <h2>{item.label}</h2>
              <p>{item.description}</p>
            </a>
          )),
        )}
      </div>
    </>
  );
}
