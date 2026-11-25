import type { Icon } from "./types";
import Other from "~icons/fluent/text-bullet-list-square-20-regular";
import CalendarEdit from "~icons/fluent/calendar-edit-20-regular";
import TextBulletListLtr from "~icons/fluent/text-bullet-list-ltr-20-regular";
import TaskListSquareLtr from "~icons/fluent/task-list-square-ltr-20-regular";
import LockClosed from "~icons/fluent/lock-closed-20-regular";
import DocumentText from "~icons/fluent/document-text-20-regular";
import Scales from "~icons/fluent/scales-20-regular";
import DataTrending from "~icons/fluent/data-trending-20-regular";
import Building from "~icons/fluent/building-20-regular";
import Briefcase from "~icons/fluent/briefcase-20-regular";
import Folder from "~icons/fluent/folder-20-regular";
import Tag from "~icons/fluent/tag-20-regular";
import Money from "~icons/fluent/money-20-regular";
import Person from "~icons/fluent/person-20-regular";
import PeopleTeam from "~icons/fluent/people-team-20-regular";
import CalendarClock from "~icons/fluent/calendar-clock-20-regular";
import MoneyCalculator from "~icons/fluent/money-calculator-20-regular";
import CalendarStar from "~icons/fluent/calendar-star-20-regular";
import WeatherMoon from "~icons/fluent/weather-moon-20-regular";
import PersonMoney from "~icons/fluent/person-money-20-regular";
import BuildingBank from "~icons/fluent/building-bank-20-regular";
import ArrowSync from "~icons/fluent/arrow-sync-20-regular";

// The Fluent icons the UI bundles, by their Fluent name: a screen names its icon (SPEC §13 Screen.icon).
const icons: Record<string, Icon> = {
  "calendar-edit": CalendarEdit, "text-bullet-list-ltr": TextBulletListLtr, "task-list-square-ltr": TaskListSquareLtr,
  "lock-closed": LockClosed, "document-text": DocumentText, scales: Scales, "data-trending": DataTrending, building: Building,
  briefcase: Briefcase, folder: Folder, tag: Tag, money: Money, person: Person, "people-team": PeopleTeam,
  "calendar-clock": CalendarClock, "money-calculator": MoneyCalculator, "calendar-star": CalendarStar, "weather-moon": WeatherMoon,
  "person-money": PersonMoney, "building-bank": BuildingBank, "arrow-sync": ArrowSync,
};

export const icon = (name?: string): Icon => (name && icons[name]) || Other;
