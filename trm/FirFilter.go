// Copyright (c) 2019, The GoKi Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/***************************************************************************
 *  Copyright 1991, 1992, 1993, 1994, 1995, 1996, 2001, 2002               *
 *    David R. Hill, Leonard Manzara, Craig Schock                         *
 *                                                                         *
 *  This program is free software: you can redistribute it and/or modify   *
 *  it under the terms of the GNU General Public License as published by   *
 *  the Free Software Foundation, either version 3 of the License, or      *
 *  (at your option) any later version.                                    *
 *                                                                         *
 *  This program is distributed in the hope that it will be useful,        *
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of         *
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the          *
 *  GNU General Public License for more details.                           *
 *                                                                         *
 *  You should have received a copy of the GNU General Public License      *
 *  along with this program.  If not, see <http://www.gnu.org/licenses/>.  *
 ***************************************************************************/
// 2014-09
// This file was copied from Gnuspeech and modified by Marcelo Y. Matuda.

// 2019-02
// This is a port to golang of the C++ Gnuspeech port by Marcelo Y. Matuda

package trm

import (
	"github.com/chewxy/math32"
)

const Limit = 200

type FirFilter struct {
	Ptr   int
	NTaps int
	Data  []float32
	Coef  []float32
}

func (ff *FirFilter) Init(beta, gamma, cutoff float32) {
	coefficients := make([]float32, Limit+1)

	var nCoefficients int

	// determine ideal low pass filter coefficients
	ff.MaximallyFlat(beta, gamma, &nCoefficients, coefficients)

	// trim low-value coefficients
	ff.Trim(cutoff, &nCoefficients, coefficients)

	// determine the number of taps in the filter
	nTaps := (nCoefficients * 2) - 1

	// initialize the coefficients
	increment := -1
	pointer := nCoefficients
	for i := 0; i < nTaps; i++ {
		ff.Coef[i] = coefficients[pointer]
		pointer += increment
		if pointer <= 0 {
			pointer = 2
			increment = 1
		}
	}
	ff.Ptr = 0
}

//Reset resets the data and sets the pointer to first element
func (ff *FirFilter) Reset() {
	for i := 0; i < len(ff.Data); i++ {
		ff.Data[i] = 0.0
	}
	ff.Ptr = 0
}

// MaximallyFlat Calculates coefficients for a linear phase lowpass FIR
// filter, with beta being the center frequency of the transition band (as a fraction
// of the sampling frequency), and gamme the width of the transition band
func (ff *FirFilter) MaximallyFlat(beta, gamma float32, np *int, coefficients []float32) int {
	a := make([]float32, Limit+1)
	c := make([]float32, Limit+1)

	//  initialize number of points
	*np = 0

	// cut-off frequency must be between 0 hz and nyquist
	if beta <= 0.0 || beta >= 0.5 {
		return 0
		// THROW_EXCEPTION(TRMException, "Beta out of range.");
	}

	// transition band must fit with the stop band
	betaMin := 2.0 * beta
	if betaMin < 1.0-2.0*beta {
		betaMin = 2.0 * beta
	} else {
		betaMin = 1.0 - 2.0*beta
	}
	if gamma <= 0.0 || gamma >= betaMin {
		return 0
		// THROW_EXCEPTION(TRMException, "Gamma out of range.");
	}

	// make sure transition band not too small
	nt := int(1.0 / (4.0 * gamma * gamma))
	if nt > 160 {
		return 0
		// THROW_EXCEPTION(TRMException, "Gamma too small.");
	}

	// calculate the rational approximation to the cut-off point
	ac := 1.0 + math32.Cos((2.0*math32.Pi)*beta)/2.0
	var numerator int
	RationalApproximation(ac, &nt, &numerator, np)

	// calculate filter order
	n := (2 * (*np)) - 1
	if numerator == 0 {
		numerator = 1
	}

	// compute magnitude at np points
	a[1] = 1.0
	c[1] = 1.0
	ll := nt - numerator

	for i := 2; i <= *np; i++ {
		var sum float32 = 1.0
		c[i] = math32.Cos(2.0 * math32.Pi * float32(i-1) / float32(n))
		x := (1.0 - c[i]) / 2.0
		y := x

		if numerator == nt {
			continue
		}

		for j := 1; j <= ll; j++ {
			z := y
			if numerator != 1 {
				for jj := 1; jj <= numerator-1; jj++ {
					z *= 1.0 + float32(j)/float32(jj)
				}
			}
			y *= x
			sum += float32(z)
		}
		a[i] = sum * math32.Pow(float32(1.0-x), float32(numerator))
	}

	// Calculate weighting coefficients by an n-point idft
	for i := 1; i <= *np; i++ {
		ff.Coef[i] = a[1] / 2.0
		for j := 2; j <= *np; j++ {
			m := ((i - 1) * (j - 1)) % n
			if m > nt {
				m = n - m
			}
			ff.Coef[i] += c[m+1] * a[j]
		}
		ff.Coef[i] *= 2.0 / float32(n)
	}
	return 0
}

// Trims the higher order coefficients of the FIR filter which fall below the cutoff value
func (ff *FirFilter) Trim(cutoff float32, nCoefficients *int, coefficients []float32) {
	for i := *nCoefficients; i > 0; i-- {
		if math32.Abs(ff.Coef[i]) >= math32.Abs(cutoff) {
			*nCoefficients = i
			return
		}
	}

}

func (ff *FirFilter) Filter(input float32, needOutput bool) float32 {
	if needOutput {
		var output float32 = 0.0

		// put input sample into data buffer
		ff.Data[ff.Ptr] = input

		// sum the output from all filter taps
		for i := 0; i < ff.NTaps; i++ {
			output += ff.Data[ff.Ptr] * ff.Coef[i]
			ff.Ptr = Increment(ff.Ptr, ff.NTaps)
		}
		// decrement the data pointer ready for next call
		ff.Ptr = Decrement(ff.Ptr, ff.NTaps)
		return output
	} else {
		// put input sample into data buffer
		ff.Data[ff.Ptr] = input

		// adjust the data pointer, ready for next call
		ff.Ptr = Decrement(ff.Ptr, ff.NTaps)

		return 0.0
	}
}

// Increment Increments the pointer to the circular FIR filter buffer, keeping it in the range 0 -> modulus-1
func Increment(ptr, modulus int) int {
	ptr += 1
	if ptr >= modulus {
		return 0
	} else {
		return ptr
	}
}

// Decrement decrements the pointer to the circular FIR filter buffer, keeping it in the range 0 -> modulus-1
func Decrement(ptr, modulus int) int {
	ptr -= 1
	if ptr < 0 {
		return modulus - 1
	} else {
		return ptr
	}
}

// RationalApproximation  calculates the best rational approximation to 'number', given the maximum 'order'.
func RationalApproximation(number float32, order, numerator, denominator *int) {
	var minimumError float32 = 1.0
	var modulus int = 0

	// return immediately if the order is less than one
	if *order <= 0 {
		*numerator = 0
		*denominator = 0
		*order = -1
		return
	}

	// find the absolute value of the fractional part of the number
	fractionalPart := math32.Abs(number - float32(int(number)))

	// determine the maximum value of the denominator
	orderMaximum := 2 * (*order)
	if orderMaximum > Limit {
		orderMaximum = Limit
	}

	//  find the best denominator value
	for i := (*order); i <= orderMaximum; i++ {
		ps := float32(i) * fractionalPart
		ip := int(ps + 0.5)
		error := math32.Abs((ps - float32(ip)) / float32(i))
		if error < minimumError {
			minimumError = error
			modulus = ip
			*denominator = i
		}
	}

	// determine the numerator value, making it negative if necessary
	*numerator = int(int(math32.Abs(number))*(*denominator) + modulus)
	if number < 0 {
		*numerator *= -1
	}

	*order = *denominator - 1

	// reset the numerator and denominator if they are equal
	if *numerator == *denominator {
		*denominator = orderMaximum
		*numerator = *denominator - 1
		*order = *numerator
	}
}
