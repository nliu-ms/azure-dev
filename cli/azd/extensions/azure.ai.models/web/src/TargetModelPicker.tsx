import { Badge } from "@fluentui/react-components";
import { targetModelKey, type TargetModelChoice } from "./targetModels";

type Props = {
  choices: TargetModelChoice[];
  selected: TargetModelChoice | null;
  suggested: TargetModelChoice | null;
  onChange: (target: TargetModelChoice) => void;
};

export function TargetModelPicker({ choices, selected, suggested, onChange }: Props) {
  const isSuggested = selected && suggested && targetModelKey(selected) === targetModelKey(suggested);
  return (
    <div className="target-model-picker">
      <div className="target-model-picker-heading">
        <strong>Choose Target model</strong>
        {selected && <Badge appearance="tint" color="informative">{isSuggested ? "Suggested default" : "Your choice"}</Badge>}
      </div>
      <label>
        <span>Target model</span>
        <select
          aria-label="Target model"
          value={selected ? targetModelKey(selected) : ""}
          onChange={(event) => {
            const target = choices.find((choice) => targetModelKey(choice) === event.target.value);
            if (target) onChange(target);
          }}
        >
          <option value="" disabled>Select a model</option>
          {choices.map((choice) => (
            <option key={targetModelKey(choice)} value={targetModelKey(choice)}>
              {choice.modelName} · {choice.modelFormat}
              {suggested && targetModelKey(choice) === targetModelKey(suggested) ? " — suggested" : ""}
            </option>
          ))}
        </select>
      </label>
      <p>The suggestion is a starting point, not a restriction. Other listed choices come from discovered deployments.</p>
    </div>
  );
}
