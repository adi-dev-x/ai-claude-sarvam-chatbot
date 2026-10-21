// Package chat answers business questions with Claude, using insight queries as tools.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
	"github.com/anthropics/anthropic-sdk-go/toolrunner"

	"aichatbot/internal/backend"
)

const (
	chatModel        = "claude-opus-5-5"
	chatMaxRows      = 20
	chatSystemPrompt = `You are the voice assistant for the owner of a scaffolding and equipment rental business.
The owner asks about performance, income, customers, payments, equipment and returns.

How the data works:
- Rental income is earned per day: each rented unit earns its daily rent for every day it is out, up to its planned end date. A period's income comes from business_snapshot or income_by_equipment for that period.
- Paid on an order = advance + completed invoices. Owed = order amount minus paid.
- Invoices have no payment date and no due date, so "overdue payment" means money still owed on an order older than N days (use receivables with min_age_days, 30 if not specified).
- The only recorded cost is repair cost. "Profit" can only mean rental income minus repair cost; say so when you use it.
- Not recorded at all: general business expenses (salaries, fuel, rent), cash or bank balance, equipment purchase price, maintenance schedules. For questions that need these, say plainly that this information is not recorded, and offer what the data can tell instead.

How to answer:
- Use tools for every number; never guess. Compare periods by calling a tool once per period.
- For forecasts or advice (next month's revenue, pricing, what to buy or replace), base it on the trends and utilization the tools return and say it is an estimate.
- Questions may be in Malayalam, Hindi, English or another Indian language. Write the answer in the language the request names, in its own script, keeping customer names, order numbers and product codes as they appear in the data.
- Amounts are Indian rupees: write ₹ with Indian digit grouping (₹1,25,000).
- While reading name of (item) tool, don't spell alphabets, instead say the names of the tools also say the description of tools but don't read product code.
- The answer is converted to speech, so it is read aloud: plain sentences, no markdown, lists, tables or emoji. Give the direct answer first. Keep simple questions to one or two sentences; broad questions such as "how is my business doing" or "what needs my attention" may use up to five short sentences covering the most important points.`
)

// ist is India Standard Time; a fixed zone avoids depending on tzdata in the container.
var ist = time.FixedZone("IST", 5*60*60+30*60)

// Bot answers natural-language questions about the business. Its tools are the rental
// app's read-only report endpoints, so every figure follows the app's own rules.
type Bot struct {
	client  anthropic.Client
	reports *backend.Client
	tools   []anthropic.BetaTool
}

func New(apiKey string, reports *backend.Client) (*Bot, error) {
	bot := &Bot{
		client:  anthropic.NewClient(option.WithAPIKey(apiKey)),
		reports: reports,
	}
	tools, err := bot.buildTools()
	if err != nil {
		return nil, err
	}
	bot.tools = tools
	return bot, nil
}

// Answer runs the tool loop for one question and returns Claude's final reply,
// written in replyLanguage (e.g. "Malayalam").
func (b *Bot) Answer(ctx context.Context, question, replyLanguage string) (string, error) {
	today := time.Now().In(ist).Format("Monday, 2006-01-02")
	prompt := fmt.Sprintf("Today is %s.\nWrite the answer in %s.\n\nQuestion: %s", today, replyLanguage, question)

	runner := b.client.Beta.Messages.NewToolRunner(b.tools, anthropic.BetaToolRunnerParams{
		BetaMessageNewParams: anthropic.BetaMessageNewParams{
			Model:        chatModel,
			MaxTokens:    16000,
			System:       []anthropic.BetaTextBlockParam{{Text: chatSystemPrompt}},
			OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortMedium},
			Fallbacks:    anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
			Betas:        []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
			Messages: []anthropic.BetaMessageParam{
				anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(prompt)),
			},
		},
		MaxIterations: 8,
	})

	message, err := runner.RunToCompletion(ctx)
	if err != nil {
		return "", fmt.Errorf("chat failed: %w", err)
	}
	if message.StopReason == anthropic.BetaStopReasonRefusal {
		return "", fmt.Errorf("the question was declined")
	}

	var answer strings.Builder
	for _, block := range message.Content {
		if text, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			answer.WriteString(text.Text)
		}
	}
	return strings.TrimSpace(answer.String()), nil
}

type noInput struct{}

type periodInput struct {
	From string `json:"from" jsonschema:"required,description=First day of the period, YYYY-MM-DD"`
	To   string `json:"to" jsonschema:"required,description=Last day of the period (inclusive), YYYY-MM-DD"`
}

type equipmentIncomeInput struct {
	From  string `json:"from" jsonschema:"required,description=First day of the period, YYYY-MM-DD"`
	To    string `json:"to" jsonschema:"required,description=Last day of the period (inclusive), YYYY-MM-DD"`
	Order string `json:"order" jsonschema:"enum=highest,enum=lowest,description=highest income first (default) or lowest income first"`
}

type limitInput struct {
	Limit int `json:"limit" jsonschema:"description=How many rows to return (1-20). Use 1 for 'the highest'."`
}

type daysInput struct {
	Days int `json:"days" jsonschema:"required,description=Number of days"`
}

type customerInput struct {
	Name  string `json:"name" jsonschema:"required,description=Full or partial customer name"`
	Limit int    `json:"limit" jsonschema:"description=How many recent orders to return (1-20)"`
}

type customerDirectoryInput struct {
	Name  string `json:"name" jsonschema:"description=Full or partial customer name, short name or phone. Leave empty for all customers."`
	Limit int    `json:"limit" jsonschema:"description=How many customers to list (1-20)"`
}

type customerActivityInput struct {
	Sort string `json:"sort" jsonschema:"required,enum=revenue,enum=inactive,description=revenue: highest lifetime revenue first. inactive: longest time since last order first"`
}

type revenueTrendInput struct {
	Period string `json:"period" jsonschema:"required,enum=daily,enum=weekly,enum=monthly,description=Bucket size"`
	Limit  int    `json:"limit" jsonschema:"description=How many recent periods to return (1-20)"`
}

type receivablesInput struct {
	MinAgeDays int `json:"min_age_days" jsonschema:"required,description=Only count orders at least this many days old. 0 for all."`
}

func (b *Bot) buildTools() ([]anthropic.BetaTool, error) {
	period := func(in periodInput) url.Values { return url.Values{"from": {in.From}, "to": {in.To}} }
	none := func(noInput) url.Values { return nil }

	builders := []func() (anthropic.BetaTool, error){
		newReportTool(b, "business_snapshot", "snapshot",
			"Headline numbers for a period: rental income earned, orders placed and their value, advances received, cancelled orders, repair events and repair cost. "+
				"Also the current state: units on rent and available, units overdue for return, total owed by customers and how many customers owe. "+
				"Use for today, this month, last month, and period comparisons (call once per period).",
			period),
		newReportTool(b, "income_by_equipment", "income-by-equipment",
			"Rental income, rented unit-days and utilization % per product for a period, with units owned. Use for most/least earning equipment, demand and utilization.",
			func(in equipmentIncomeInput) url.Values {
				return url.Values{"from": {in.From}, "to": {in.To}, "order": {in.Order}}
			}),
		newReportTool(b, "fleet_status", "fleet-status",
			"Current unit counts per product: total, available, rented, reserved, at site, damaged, repairing, blocked, worn out, and utilization now. The first row is the total for all products.",
			none),
		newReportTool(b, "idle_equipment", "idle-equipment",
			"Per product, available units that have not been rented in the last N days (including never rented), with the oldest last-rented date.",
			func(in daysInput) url.Values { return url.Values{"days": {strconv.Itoa(in.Days)}} }),
		newReportTool(b, "receivables", "receivables",
			"Money owed per customer on orders at least min_age_days old (0 for everything), highest first, with pending invoice amounts and the age of the oldest unpaid order. The first row is the total for all customers.",
			func(in receivablesInput) url.Values { return url.Values{"min_age_days": {strconv.Itoa(in.MinAgeDays)}} }),
		newReportTool(b, "customer_payment_history", "customer-payment-history",
			"Recent orders of a customer (matched by partial name) with order amount, advance, invoices paid and pending, amount owed and order age.",
			func(in customerInput) url.Values {
				return url.Values{"name": {in.Name}, "limit": {strconv.Itoa(in.Limit)}}
			}),
		newReportTool(b, "largest_orders", "largest-orders",
			"Individual orders ranked by order amount, largest first, with customer, amount, paid, owed, status and date.",
			func(in limitInput) url.Values { return url.Values{"limit": {strconv.Itoa(in.Limit)}} }),
		newReportTool(b, "rental_returns", "rental-returns",
			"Rented units past their planned return date, plus units due back within the next N days (0 for overdue only). "+
				"Includes days overdue (negative means due in future), daily rent and the rent not yet billed for overdue days.",
			func(in daysInput) url.Values { return url.Values{"days": {strconv.Itoa(in.Days)}} }),
		newReportTool(b, "customer_directory", "customer-details",
			"Every registered customer, including those with no orders: total_customers (the count matching name, or of all customers when name is empty), the count per customer type, "+
				"and contact details (name, short name, phone, type, GST, address, email, flag, status) of the first ones alphabetically. "+
				"Use for how many customers there are, and for a customer's phone, address or GST.",
			func(in customerDirectoryInput) url.Values {
				return url.Values{"name": {in.Name}, "limit": {strconv.Itoa(in.Limit)}}
			}),
		newReportTool(b, "customer_activity", "customer-activity",
			"Per customer with live orders: lifetime orders, revenue, amount owed, last order date and days since last order. Use for most valuable, lapsed and follow-up customers. "+
				"Customers without orders are not listed, so do not use it to count customers.",
			func(in customerActivityInput) url.Values { return url.Values{"sort": {in.Sort}} }),
		newReportTool(b, "repair_vs_income", "repair-vs-income",
			"Per product for a period: repair events, units repaired, repair cost, rental income, repair cost as % of income, and units damaged or in repair now. Only products with repairs or damage are listed.",
			period),
		newReportTool(b, "revenue_trend", "revenue-trend",
			"Value of orders placed per day, week or month for recent periods. Useful for trends and rough forecasts.",
			func(in revenueTrendInput) url.Values {
				return url.Values{"period": {in.Period}, "limit": {strconv.Itoa(in.Limit)}}
			}),
		newReportTool(b, "order_status_summary", "order-status-summary",
			"Number of orders in each status.",
			none),
		newReportTool(b, "customer_damage_stats", "customer-damage",
			"Damage counts per customer.",
			none),
	}

	tools := make([]anthropic.BetaTool, 0, len(builders))
	for _, build := range builders {
		tool, err := build()
		if err != nil {
			return nil, fmt.Errorf("failed to build chat tool: %w", err)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// newReportTool exposes one rental app report as a tool; params turns the tool input into query parameters.
func newReportTool[T any](b *Bot, name, report, description string, params func(T) url.Values) func() (anthropic.BetaTool, error) {
	return func() (anthropic.BetaTool, error) {
		return toolrunner.NewBetaToolFromJSONSchema(name, description,
			func(ctx context.Context, in T) (anthropic.BetaToolResultBlockParamContentUnion, error) {
				data, err := b.reports.Report(ctx, report, params(in))
				log.Printf("chat tool %s input=%+v err=%v", name, in, err)
				if err != nil {
					return anthropic.BetaToolResultBlockParamContentUnion{}, err
				}
				return anthropic.BetaToolResultBlockParamContentUnion{
					OfText: &anthropic.BetaTextBlockParam{Text: truncateRows(data)},
				}, nil
			})
	}
}

// truncateRows keeps at most chatMaxRows rows of a JSON array, noting how many there were.
func truncateRows(data json.RawMessage) string {
	var rows []json.RawMessage
	if json.Unmarshal(data, &rows) != nil || len(rows) <= chatMaxRows {
		return string(data)
	}
	out, err := json.Marshal(map[string]any{
		"rows":       rows[:chatMaxRows],
		"total_rows": len(rows),
		"note":       fmt.Sprintf("only the first %d rows are shown", chatMaxRows),
	})
	if err != nil {
		return string(data)
	}
	return string(out)
}
