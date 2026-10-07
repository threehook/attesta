// What a claim is called to the user. Claims this wallet does not know are shown by their own name.
const LABELS: Record<string, string> = {
  name: "Naam",
  email: "E-mailadres",
  degree: "Graad",
  university: "Opleidingsinstituut",
  department: "Afdeling",
  organisation: "Organisatie",
  diploma: "Diploma",
  description: "Omschrijving",
};

export const claimLabel = (claim: string): string => LABELS[claim] ?? claim;
