package store

import "github.com/lieranderl/aestheticlab-income-app/internal/domain"

// DefaultSetup is the salon's first data (technical-solution §9): the admin,
// the default revenue categories, payment methods, expense categories and
// the two kassa accounts. Every revenue category counts toward «Себе»
// except certificate sales, which go to the kassa in full.
func DefaultSetup(admin NewUser) Setup {
	return Setup{
		Admin: admin,
		Categories: []Category{
			{ID: "services", Name: "Услуги", Kind: KindRevenue, Order: 1, CountsTowardSelf: true},
			{ID: "creams", Name: "Кремы", Kind: KindRevenue, Order: 2, CountsTowardSelf: true},
			{ID: "certificates", Name: "Сертификаты", Kind: KindRevenue, Order: 3, IsCertificateSale: true},
			{ID: "goods", Name: "Другие товары", Kind: KindRevenue, Order: 4, CountsTowardSelf: true},
			{ID: "other", Name: "Прочее", Kind: KindRevenue, Order: 5, CountsTowardSelf: true},

			{ID: "card", Name: "Карта", Kind: KindPayment, Order: 1, Destination: domain.ToKassaCard},
			{ID: "cash", Name: "Наличные", Kind: KindPayment, Order: 2, Destination: domain.ToCash},
			{ID: "paid_by_certificate", Name: "Сертификат", Kind: KindPayment, Order: 3, Destination: domain.Prepaid},

			{ID: "rent", Name: "Аренда", Kind: KindExpense, Order: 1},
			{ID: "materials", Name: "Материалы", Kind: KindExpense, Order: 2},
			{ID: "utilities", Name: "Коммунальные (электричество, интернет)", Kind: KindExpense, Order: 3},
			{ID: "marketing", Name: "Реклама", Kind: KindExpense, Order: 4},
			{ID: "household", Name: "Хозтовары", Kind: KindExpense, Order: 5},
			{ID: "equipment", Name: "Оборудование и мебель", Kind: KindExpense, Order: 6},
			{ID: "software", Name: "Софт и хостинг", Kind: KindExpense, Order: 7},
			{ID: "expense_other", Name: "Прочее", Kind: KindExpense, Order: 8},
		},
		Accounts: []Account{
			{ID: domain.KassaCash, Name: "Касса — наличные", Kind: AccountCash, Order: 1},
			{ID: domain.KassaCard, Name: "Касса — карта салона", Kind: AccountCard, Order: 2},
		},
	}
}
