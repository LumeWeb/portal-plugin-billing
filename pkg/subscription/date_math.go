// Package subscription provides domain types and utilities for subscription billing cycles,
// pricing, and date/time calculations.
package subscription

import (
	"time"
)

// DaysBetween calculates the number of days between two dates.
//
// It returns the integer division of the duration in hours by 24, which yields
// the number of whole days between the start and end times.
//
// Same day returns 0.
// Next day returns 1.
// Returns negative if end is before start.
//
// Parameters:
//   - start: The start date and time.
//   - end: The end date and time.
//
// Returns:
//   - The number of days between start and end.
func DaysBetween(start, end time.Time) int {
	return int(end.Sub(start).Hours() / 24)
}

// EndOfDay returns the end of day time for the given time.
//
// It returns a new time.Time value set to 23:59:59.999999 on the same day
// as the input time, preserving the original location/timezone.
//
// Parameters:
//   - t: The time for which to get the end of day.
//
// Returns:
//   - The end of day (23:59:59.999999) for the same day as the input time.
func EndOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999, t.Location())
}

// StartOfDay returns the start of day time for the given time.
//
// It returns a new time.Time value set to 00:00:00.0 on the same day
// as the input time, preserving the original location/timezone.
//
// Parameters:
//   - t: The time for which to get the start of day.
//
// Returns:
//   - The start of day (00:00:00.0) for the same day as the input time.
func StartOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// DaysInMonth returns the number of days in the month for the given time.
//
// It returns the total days in the month by constructing a date for the first day
// of the next month with day 0, which resolves to the last day of the current month,
// and then extracting the day number.
//
// This is a pure function with no side effects.
//
// Parameters:
//   - t: The time for which to get the number of days in the month.
//
// Returns:
//   - The number of days in the month (28-31) for the month of the input time.
func DaysInMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
}

// EndOfMonth returns the end of month time for the given time.
//
// It returns a new time.Time value set to the last day of the month at 23:59:59.999999,
// preserving the original location/timezone. This works by constructing a date for the
// first day of the next month with day 0, which resolves to the last day of the
// previous month.
//
// Parameters:
//   - t: The time for which to get the end of month.
//
// Returns:
//   - The end of month (last day, 23:59:59.999999) for the same month as the input time.
func EndOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month()+1, 0, 23, 59, 59, 999999, t.Location())
}

// StartOfMonth returns the start of month time for the given time.
//
// It returns a new time.Time value set to the first day of the month at 00:00:00.0,
// preserving the original location/timezone.
//
// Parameters:
//   - t: The time for which to get the start of month.
//
// Returns:
//   - The start of month (first day, 00:00:00.0) for the same month as the input time.
func StartOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// AddMonths adds the specified number of months to the given time.
//
// It returns a new time.Time value with the specified number of months added,
// preserving the time of day and location/timezone from the input time. If the
// month overflows (e.g., adding 2 months to November), it will advance the year
// appropriately.
//
// Parameters:
//   - t: The base time to which months will be added.
//   - months: The number of months to add; can be negative to subtract months.
//
// Returns:
//   - A new time.Time value with the specified number of months added.
func AddMonths(t time.Time, months int) time.Time {
	return t.AddDate(0, months, 0)
}


// IsSameDay compares two times and returns whether they occur on the same day.
//
// This function checks only the date components (year, month, and day) and ignores
// time components (hour, minute, second, nanosecond) and timezone/location comparisons.
//
// Parameters:
//   - a: The first time to compare.
//   - b: The second time to compare.
//
// Returns:
//   - true if both times represent the same calendar day, false otherwise.
func IsSameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

// YearsInPeriod calculates the number of years between two dates.
//
// It returns the difference in years between the end and start dates by
// subtracting the year of the start date from the year of the end date.
//
// Same year returns 0.
// Positive values indicate the end year is after the start year.
// Negative values indicate the end year is before the start year.
//
// This is a pure function with no side effects.
//
// Parameters:
//   - start: The start date and time.
//   - end: The end date and time.
//
// Returns:
//   - The difference in years between end and start.
func YearsInPeriod(start, end time.Time) int {
	return end.Year() - start.Year()
}

// MonthsInPeriod calculates the number of months between two dates.
//
// It returns the difference in months between the end and start dates by
// converting each date to a month count (year * 12 + month) and computing
// the difference.
//
// Same month returns 0.
// Positive values indicate the end month is after the start month.
// Negative values indicate the end month is before the start month.
//
// This is a pure function with no side effects.
//
// Parameters:
//   - start: The start date and time.
//   - end: The end date and time.
//
// Returns:
//   - The difference in months between end and start.
func MonthsInPeriod(start, end time.Time) int {
	return (end.Year()*12 + int(end.Month())) - (start.Year()*12 + int(start.Month()))
}
