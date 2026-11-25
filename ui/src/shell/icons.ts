import type { Icon } from "../ds/types";
import Time from "~icons/fluent/clock-20-regular";
import Billing from "~icons/fluent/receipt-20-regular";
import Analysis from "~icons/fluent/data-bar-vertical-20-regular";
import MasterData from "~icons/fluent/database-20-regular";
import System from "~icons/fluent/settings-20-regular";
import Entry from "~icons/fluent/calendar-edit-20-regular";
import List from "~icons/fluent/text-bullet-list-ltr-20-regular";
import Validate from "~icons/fluent/task-list-square-ltr-20-regular";
import Lock from "~icons/fluent/lock-closed-20-regular";
import Invoice from "~icons/fluent/document-text-20-regular";
import Balance from "~icons/fluent/scales-20-regular";
import Trend from "~icons/fluent/data-trending-20-regular";
import Account from "~icons/fluent/building-20-regular";
import Project from "~icons/fluent/briefcase-20-regular";
import Folder from "~icons/fluent/folder-20-regular";
import Task from "~icons/fluent/tag-20-regular";
import Money from "~icons/fluent/money-20-regular";
import Person from "~icons/fluent/person-20-regular";
import Team from "~icons/fluent/people-team-20-regular";
import Period from "~icons/fluent/calendar-clock-20-regular";
import Cost from "~icons/fluent/money-calculator-20-regular";
import Holiday from "~icons/fluent/calendar-star-20-regular";
import Night from "~icons/fluent/weather-moon-20-regular";
import Rate from "~icons/fluent/person-money-20-regular";
import Company from "~icons/fluent/building-bank-20-regular";
import Sync from "~icons/fluent/arrow-sync-20-regular";

// Presentation of the slice's navigation; a screen without an entry shows its group's icon.
export const groupIcons: Record<string, Icon> = { time: Time, billing: Billing, analysis: Analysis, master_data: MasterData, system: System };

export const screenIcons: Record<string, Icon> = {
  entry: Entry, activities: List, validation: Validate, locks: Lock, invoicing: Invoice, balances: Balance, profitability: Trend,
  account: Account, project: Project, folder: Folder, task: Task, expense_type: Money, user: Person, user_group: Team,
  work_period: Period, cost_rate: Cost, public_holiday: Holiday, hour_class: Night, user_task_rate: Rate, company: Company,
  accounting: Sync,
};
