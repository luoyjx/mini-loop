import { ResearchIndex } from "./research-index";
import { researchIndex } from "./research-data";

export default function Home() {
  return <ResearchIndex documents={researchIndex} />;
}
