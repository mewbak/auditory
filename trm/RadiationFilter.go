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

type RadiationFilter struct {
	A20        float32
	A21        float32
	B21        float32
	RadiationX float32
	RadiationY float32
}

func (rf *RadiationFilter) Init(apertureCoef float32) {
	rf.RadiationX = 0.0
	rf.RadiationY = 0.0
	rf.A20 = apertureCoef
	rf.A21 = rf.A20
	rf.B21 = rf.A20
}

func (rf *RadiationFilter) Reset() {
	rf.RadiationX = 0.0
	rf.RadiationY = 0.0
}

func (rf *RadiationFilter) Filter(input float32) float32 {
	output := (rf.A20 * input) + (rf.A21 * rf.RadiationX) - (rf.B21 * rf.RadiationY)
	rf.RadiationX = input
	rf.RadiationY = output
	return output
}
