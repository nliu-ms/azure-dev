export function uniqueEvidenceNotes(notes: readonly string[]): string[] {
  return [...new Set(notes.map((note) => note.trim()).filter(Boolean))];
}

export function EvidenceNotes({ notes, title = "Data notes" }: { notes: readonly string[]; title?: string }) {
  const values = uniqueEvidenceNotes(notes);
  if (values.length === 0) return null;
  return (
    <details className="evidence-notes">
      <summary>{title} ({values.length})</summary>
      <ul>{values.map((note) => <li key={note}>{note}</li>)}</ul>
    </details>
  );
}
